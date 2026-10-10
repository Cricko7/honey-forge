package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"honey-forge/internal/contract"
)

// RecoverSessions closes sessions interrupted by a previous process exit. Only
// bytes already durably observed are claimed after a crash.
func (j *Journal) RecoverSessions() error {
	j.mu.Lock()
	active := make([]activeSession, 0, len(j.active))
	for _, a := range j.active {
		active = append(active, a)
	}
	j.mu.Unlock()
	for _, a := range active {
		var data struct {
			Listener string `json:"listener_name"`
			Service  string `json:"service"`
		}
		if err := json.Unmarshal(a.Last.Data, &data); err != nil {
			return err
		}
		fields := map[string]any{"duration_ms": max(int64(0), time.Since(a.Started).Milliseconds()), "bytes_received": a.Bytes, "reason": "service_stopped"}
		kind := "tcp.connection_closed"
		if strings.HasPrefix(a.Last.EventType, "service.") || a.Last.EventType == "honeytoken.triggered" {
			kind = "service.connection_closed"
			fields["service"] = data.Service
		} else {
			fields["listener_name"] = data.Listener
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		e := a.Last
		e.EventID = string(contract.NewID())
		e.EventType = kind
		e.SessionSequence++
		e.OccurredAt = time.Now().UTC().Format(time.RFC3339Nano)
		e.Data = raw
		if _, err := j.Enqueue(e); err != nil {
			return fmt.Errorf("close recovered session: %w", err)
		}
	}
	return nil
}
