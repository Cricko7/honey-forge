package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
)

func TestJournalRepairsUncommittedTailAndLocksFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	j, err := OpenJournal(path, "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenJournal(path, "trap", 1<<20); err == nil {
		other.Close()
		t.Fatal("second writer acquired journal")
	}
	j.Close()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"batch":`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	j, err = OpenJournal(path, "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	j.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if raw[len(raw)-1] != '\n' {
		t.Fatal("tail not repaired")
	}
}

func TestJournalConcurrentEventsAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	j, err := OpenJournal(path, "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Go(func() {
			_, err := j.Enqueue(events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), EventType: "tcp.connection_opened", SessionSequence: 1, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Data: json.RawMessage(`{"listener_name":"demo"}`)})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	j.Close()
	j, err = OpenJournal(path, "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.RecoverSessions(); err != nil {
		t.Fatal(err)
	}
	if len(j.Pending()) != 32 {
		t.Fatalf("recovered %d events", len(j.Pending()))
	}
	if err := j.RecoverSessions(); err != nil {
		t.Fatal(err)
	}
	if len(j.Pending()) != 32 {
		t.Fatal("recovery duplicated close events")
	}
}

func TestJournalCapacityPreservesRoomForSessionClose(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 32*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	e := events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), EventType: "tcp.connection_opened", Data: json.RawMessage(`{"listener_name":"demo"}`)}
	if _, err := j.Enqueue(e); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 1000)
	for i := range payload {
		payload[i] = 'a'
	}
	raw, err := json.Marshal(map[string]any{"payload": string(payload)})
	if err != nil {
		t.Fatal(err)
	}
	e.EventType = "tcp.payload_received"
	e.Data = raw
	full := false
	for range 100 {
		e.EventID = string(contract.NewID())
		if _, err := j.Enqueue(e); err != nil {
			full = true
			break
		}
	}
	if !full {
		t.Fatal("capacity not enforced")
	}
	e.EventType = "tcp.connection_closed"
	e.EventID = string(contract.NewID())
	e.Data = json.RawMessage(`{"reason":"service_stopped"}`)
	if _, err := j.Enqueue(e); err != nil {
		t.Fatalf("reserved close could not commit: %v", err)
	}
}
