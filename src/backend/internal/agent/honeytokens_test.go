package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
)

func TestJournalRecoversHoneytokenSession(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "tokens.journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	e := events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), SessionSequence: 1, TypeID: "honeytoken-http", EventType: "service.connection_opened", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Data: json.RawMessage(`{"service":"web"}`)}
	if _, err := j.Enqueue(e); err != nil {
		t.Fatal(err)
	}
	e.EventID = string(contract.NewID())
	e.SessionSequence = 2
	e.EventType = "honeytoken.triggered"
	e.Data = json.RawMessage(`{"service":"web","token_id":"backup","kind":"key","method":"GET"}`)
	if _, err := j.Enqueue(e); err != nil {
		t.Fatal(err)
	}
	if err := j.RecoverSessions(); err != nil {
		t.Fatal(err)
	}
	pending := j.Pending()
	if len(pending) != 3 || pending[2].Events[0].SessionSequence != 3 || pending[2].Events[0].EventType != "service.connection_closed" || !containsService(pending[2].Events[0].Data) {
		t.Fatalf("recovery: %+v", pending)
	}
}

func containsService(raw json.RawMessage) bool {
	var data struct {
		Service string `json:"service"`
	}
	return json.Unmarshal(raw, &data) == nil && data.Service == "web"
}
