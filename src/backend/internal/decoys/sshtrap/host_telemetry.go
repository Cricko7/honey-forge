package sshtrap

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"honey-forge/internal/hosttelemetry"
)

type sandboxBinding struct {
	record  *recording
	started time.Time
	seen    map[string]bool
}

type hostObserver struct {
	host, trap string
	generation int64
	mu         sync.Mutex // serializes bindings, replay suppression and delivery
	bindings   map[string]*sandboxBinding
	ignored    atomic.Uint64
	accepted   atomic.Uint64
}

func newHostObserver(host, trap string, generation int64) *hostObserver {
	return &hostObserver{host: host, trap: trap, generation: generation, bindings: map[string]*sandboxBinding{}}
}

func (o *hostObserver) bind(id string, started time.Time, record *recording) error {
	if !hosttelemetry.ContainerMatches(id, id) || started.IsZero() || record == nil || record.snapshot.TypeVersion != 2 {
		return fmt.Errorf("invalid inspected sandbox binding")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, exists := o.bindings[id]; exists {
		return fmt.Errorf("sandbox identity already bound")
	}
	o.bindings[id] = &sandboxBinding{record: record, started: started, seen: map[string]bool{}}
	return nil
}

func (o *hostObserver) unbind(session string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for id, binding := range o.bindings {
		if binding.record.sessionID == session {
			delete(o.bindings, id)
		}
	}
}

func (o *hostObserver) accept(event *hosttelemetry.Exec) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	var match *sandboxBinding
	var container string
	for id, binding := range o.bindings {
		if hosttelemetry.ContainerMatches(event.ContainerID, id) {
			if match != nil {
				o.ignored.Add(1)
				return nil // ambiguous truncated IDs are never assigned
			}
			match, container = binding, id
		}
	}
	if match == nil || event.At.Before(match.started) || match.record.ctx.Err() != nil {
		o.ignored.Add(1)
		return nil
	}
	if match.seen[event.ExecID] {
		return nil
	}

	fields := map[string]any{
		"schema_version":       1,
		"sensor":               "tetragon",
		"sensor_version":       "1.7.0",
		"host_id":              o.host,
		"generation":           o.generation,
		"container_id":         container,
		"container_started_at": match.started.UTC().Format(time.RFC3339Nano),
		"pid":                  event.PID,
		"ppid":                 event.PPID,
		"uid":                  event.UID,
		"binary":               event.Binary,
		"cwd":                  event.CWD,
		"arguments_redacted":   true,
	}
	// IDs survive repeated sensor records. The recording sequence advances only
	// after the existing durable sink has acknowledged the complete envelope.
	hash := sha256.Sum256([]byte(o.host + "\x00" + container + "\x00" + fields["container_started_at"].(string) + "\x00" + event.ExecID))
	hash[6] = hash[6]&0x0f | 0x80 // UUIDv8 with a truncated SHA-256 payload
	hash[8] = hash[8]&0x3f | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", hash[:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
	record := match.record
	record.mu.Lock()
	err := record.emitAt("process.exec", fields, event.At, id)
	record.mu.Unlock()
	if err != nil {
		return fmt.Errorf("persist host execution: %w", err)
	}
	match.seen[event.ExecID] = true // bounded by the existing 8192-event session budget
	o.accepted.Add(1)
	return nil
}

func (o *hostObserver) consume(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), hosttelemetry.MaxRecordBytes+1)
	for scanner.Scan() {
		event, err := hosttelemetry.ParseTetragon(scanner.Bytes(), time.Now())
		if err != nil {
			return fmt.Errorf("read Tetragon execution: %w", err)
		}
		if event == nil {
			continue
		}
		if err := o.accept(event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Tetragon stream: %w", err)
	}
	return fmt.Errorf("Tetragon event stream ended")
}
