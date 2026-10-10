package agent

import (
	"encoding/json"
	"errors"
	"honey-forge/internal/contract"
	"honey-forge/modules/events"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBufferFullStopsListenersRetainsEventsAndRequiresStart(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 32<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	runner := &fakeRunner{}
	s := NewService(j, runner)
	snapshot := demoSnapshot()
	if _, err := s.Execute(t.Context(), configCommand("apply_config", &snapshot)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(t.Context(), configCommand("start", nil)); err != nil {
		t.Fatal(err)
	}
	event := events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), SessionSequence: 1, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), EventType: "tcp.connection_opened", Data: json.RawMessage(`{"listener_name":"demo"}`)}
	if _, err := j.Enqueue(event); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"listener_name": "demo", "original_bytes": 1000, "payload": strings.Repeat("a", 1000)})
	if err != nil {
		t.Fatal(err)
	}
	event.EventType = "tcp.payload_received"
	event.Data = raw
	for {
		event.EventID = string(contract.NewID())
		event.SessionSequence++
		_, err := j.Enqueue(event)
		if errors.Is(err, ErrBufferFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	count, _ := j.Usage()
	s.Failed("buffer_full")
	runtime := s.Runtime()
	if runtime.BufferState != "full" || runtime.RuntimeState != "error" || runner.stops != 1 {
		t.Fatalf("%+v stops=%d", runtime, runner.stops)
	}
	after, _ := j.Usage()
	if after != count+1 {
		t.Fatal("full buffer lost events or final session event")
	}
	for _, batch := range j.Pending() {
		ids := []string{}
		for _, e := range batch.Events {
			ids = append(ids, e.EventID)
		}
		if err := j.Ack(batch.BatchID, ids); err != nil {
			t.Fatal(err)
		}
	}
	s.RecoverBuffer()
	if runtime := s.Runtime(); runtime.BufferState != "ok" || runtime.RuntimeState != "stopped" {
		t.Fatalf("%+v", runtime)
	}
	if runner.starts != 1 {
		t.Fatal("drain silently restarted listener")
	}
	if _, err := s.Execute(t.Context(), configCommand("start", nil)); err != nil {
		t.Fatal(err)
	}
	if runner.starts != 2 {
		t.Fatal("explicit start did not resume")
	}
}
