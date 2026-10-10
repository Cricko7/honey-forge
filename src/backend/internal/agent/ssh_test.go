package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
)

func TestSSHSessionRecoveryPreservesSequenceAndInputBytes(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "ssh.journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	e := events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), SessionSequence: 1, TypeID: "ssh-high-interaction", EventType: "service.connection_opened", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Data: json.RawMessage(`{"service":"ssh"}`)}
	if _, err := j.Enqueue(e); err != nil {
		t.Fatal(err)
	}
	for i, raw := range []string{`{"service":"ssh","stream":"stdin","bytes":3}`, `{"service":"ssh","stream":"stdout","bytes":7}`, `{"service":"ssh","file_id":"x","bytes":4}`} {
		e.EventID = string(contract.NewID())
		e.SessionSequence = int64(i + 2)
		e.EventType = "service.session_io"
		if i == 2 {
			e.EventType = "service.file_chunk"
		}
		e.Data = json.RawMessage(raw)
		if _, err := j.Enqueue(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.RecoverSessions(); err != nil {
		t.Fatal(err)
	}
	last := j.Pending()[4].Events[0]
	var data struct {
		Bytes int `json:"bytes_received"`
	}
	json.Unmarshal(last.Data, &data)
	if last.EventType != "service.connection_closed" || last.SessionSequence != 5 || data.Bytes != 7 {
		t.Fatalf("recovery: %+v %s", last, string(last.Data))
	}
}
