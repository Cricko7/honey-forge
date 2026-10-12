package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
)

func TestJournalRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	j, err := OpenJournal(path, "test-trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	e := events.AgentEvent{
		EventID:   string(contract.NewID()),
		SessionID: string(contract.NewID()),
		EventType: "tcp.connection_opened",
		Data:      json.RawMessage(`{"listener_name":"demo"}`),
	}
	batch, err := j.Enqueue(e)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := j.Enqueue(e); err != nil {
		t.Fatal(err)
	}

	if len(j.Pending()) != 1 {
		t.Fatal("duplicate event")
	}

	j.Close()
	j, err = OpenJournal(path, "test-trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	if len(j.Pending()) != 1 || j.Pending()[0].BatchID != batch.BatchID {
		t.Fatal("lost stable batch")
	}

	if err := j.Ack(batch.BatchID, []string{e.EventID}); err != nil {
		t.Fatal(err)
	}

	j.Close()
	j, err = OpenJournal(path, "test-trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	defer j.Close()

	if len(j.Pending()) != 0 {
		t.Fatal("ack not persisted")
	}
}

func TestJournalRefusesWrongAckAndIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	j, err := OpenJournal(path, "one", 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	e := events.AgentEvent{
		EventID:   string(contract.NewID()),
		SessionID: string(contract.NewID()),
		EventType: "tcp.connection_opened",
		Data:      json.RawMessage(`{}`),
	}
	batch, err := j.Enqueue(e)
	if err != nil {
		t.Fatal(err)
	}

	if err := j.Ack(batch.BatchID, []string{string(contract.NewID())}); err == nil {
		t.Fatal("accepted wrong ack")
	}

	j.Close()
	if other, err := OpenJournal(path, "two", 1<<20); err == nil {
		other.Close()
		t.Fatal("accepted another trap's journal")
	}
}

func TestJournalReadResultsDoNotAliasPendingEvents(t *testing.T) {
	for _, name := range []string{"pending", "replayed enqueue"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal")
			j, err := OpenJournal(path, "test-trap", 1<<20)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				if err := j.Close(); err != nil {
					t.Error(err)
				}
			})
			event := events.AgentEvent{
				EventID:   string(contract.NewID()),
				SessionID: string(contract.NewID()),
				EventType: "tcp.connection_opened",
				Data:      json.RawMessage(`{"listener_name":"demo"}`),
			}
			batch, err := j.Enqueue(event)
			if err != nil {
				t.Fatal(err)
			}

			var read Batch
			if name == "pending" {
				read = j.Pending()[0]
			} else {
				read, err = j.Enqueue(event)
				if err != nil {
					t.Fatal(err)
				}
			}

			read.Events[0].EventID = "changed"
			read.Events[0].Data[0] = '['
			pending := j.Pending()
			if pending[0].Events[0].EventID != event.EventID || string(pending[0].Events[0].Data) != string(event.Data) {
				t.Fatal("reader changed committed telemetry")
			}

			if err := j.Close(); err != nil {
				t.Fatal(err)
			}

			j, err = OpenJournal(path, "test-trap", 1<<20)
			if err != nil {
				t.Fatal(err)
			}

			if got := j.Pending()[0]; got.BatchID != batch.BatchID || got.Events[0].EventID != event.EventID || string(got.Events[0].Data) != string(event.Data) {
				t.Fatal("journal replay differs from committed telemetry")
			}

			if err := j.Ack(batch.BatchID, []string{event.EventID}); err != nil {
				t.Fatalf("original acknowledgement no longer accepted: %v", err)
			}
		})
	}
}
