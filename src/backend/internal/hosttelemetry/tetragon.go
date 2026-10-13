// Package hosttelemetry adapts host sensor data without retaining raw arguments.
package hosttelemetry

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxRecordBytes = 64 * 1024

// Exec is the supported subset of Tetragon v1.7 execution JSON. Arguments,
// environment, node identity and labels are deliberately not decoded.
type Exec struct {
	ExecID      string
	ContainerID string
	At          time.Time
	PID         uint32
	PPID        *uint32
	UID         uint32
	Binary      string
	CWD         string
}

func ParseTetragon(raw []byte, now time.Time) (*Exec, error) {
	if len(raw) > MaxRecordBytes || !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid sensor record size or encoding")
	}

	var envelope struct {
		Exec json.RawMessage `json:"process_exec"`
		Time string          `json:"time"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode sensor envelope: %w", err)
	}
	if len(envelope.Exec) == 0 {
		return nil, nil // exit, kprobe and non-exec records are outside this adapter
	}

	var execution struct {
		Process *struct {
			ExecID string  `json:"exec_id"`
			Docker string  `json:"docker"`
			PID    *uint32 `json:"pid"`
			UID    *uint32 `json:"uid"`
			Binary string  `json:"binary"`
			CWD    string  `json:"cwd"`
		} `json:"process"`
		Parent *struct {
			PID *uint32 `json:"pid"`
		} `json:"parent"`
	}
	if err := json.Unmarshal(envelope.Exec, &execution); err != nil {
		return nil, fmt.Errorf("decode sensor execution: %w", err)
	}
	process := execution.Process
	if process == nil || process.PID == nil || *process.PID == 0 || process.UID == nil || process.ExecID == "" || len(process.ExecID) > 512 || process.Binary == "" || len(process.Binary) > 4096 || len(process.CWD) > 4096 {
		return nil, fmt.Errorf("incomplete or oversized sensor process")
	}
	at, err := time.Parse(time.RFC3339Nano, envelope.Time)
	if err != nil {
		return nil, fmt.Errorf("decode sensor time: %w", err)
	}
	if at.After(now.Add(5 * time.Minute)) {
		return nil, fmt.Errorf("sensor time exceeds allowed clock drift")
	}

	// Host processes legitimately lack container information. Do not guess a
	// container from cwd, PID, labels, parent or a Kubernetes pod name.
	if process.Docker == "" {
		return nil, nil
	}
	container := strings.TrimPrefix(process.Docker, "docker://")
	if !validContainerID(container, 12) {
		return nil, fmt.Errorf("invalid sensor Docker identity")
	}

	event := &Exec{ExecID: process.ExecID, ContainerID: container, At: at.UTC(), PID: *process.PID, UID: *process.UID, Binary: process.Binary, CWD: process.CWD}
	if execution.Parent != nil {
		event.PPID = execution.Parent.PID
	}
	return event, nil
}

func validContainerID(id string, minimum int) bool {
	if len(id) < minimum || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ContainerMatches only checks identity syntax/prefix. Callers MUST reject
// multiple matches and check the inspected incarnation's start time.
func ContainerMatches(observed, full string) bool {
	observed = strings.TrimPrefix(observed, "docker://")
	return validContainerID(full, 64) && validContainerID(observed, 12) && strings.HasPrefix(full, observed)
}
