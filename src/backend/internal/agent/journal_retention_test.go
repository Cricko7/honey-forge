package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"honey-forge/modules/commands"
	"honey-forge/modules/events"
)

func TestFileJournalCompactsAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	j, err := OpenJournal(path, "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	batch, err := j.Enqueue(events.AgentEvent{EventID: "pending-event", SessionID: "session", EventType: "tcp.connection_opened"})
	if err != nil {
		t.Fatal(err)
	}
	state := State{Running: true, Results: map[string]commands.AgentResult{"pending-command": {CommandID: "pending-command"}}, Awaiting: map[string]bool{"pending-command": true}}
	for i := 0; i < 300; i++ {
		if err := j.SaveState(state); err != nil {
			t.Fatal(err)
		}
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() > 40000 {
		t.Fatalf("journal keeps growing: %d", stat.Size())
	}
	if other, err := OpenJournal(path, "trap", 1<<20); err == nil {
		other.Close()
		t.Fatal("lost journal lock during compaction")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path, "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if !j.LoadState().Running {
		t.Fatal("checkpoint lost state")
	}
	if got := j.Pending(); len(got) != 1 || got[0].BatchID != batch.BatchID {
		t.Fatal("checkpoint lost unacknowledged event")
	}
	if len(j.active) != 1 || !j.LoadState().Awaiting["pending-command"] {
		t.Fatal("checkpoint lost session or command")
	}
	if other, err := OpenJournal(path, "other", 1<<20); err == nil {
		other.Close()
		t.Fatal("checkpoint lost identity")
	}
}

func TestJournalPrunesOnlyExpiredAcknowledgedResults(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	state := j.LoadState()
	state.Results = map[string]commands.AgentResult{"old": {}, "pending": {}, "recent": {}, "legacy": {}}
	state.Awaiting = map[string]bool{"pending": true}
	state.ResultExpiresAt = map[string]time.Time{"old": time.Now().Add(-time.Hour), "pending": time.Now().Add(-time.Hour), "recent": time.Now().Add(time.Hour)}
	if err := j.SaveState(state); err != nil {
		t.Fatal(err)
	}
	got := j.LoadState()
	if _, ok := got.Results["old"]; ok {
		t.Fatal("expired acknowledged result retained")
	}
	for _, id := range []string{"pending", "recent", "legacy"} {
		if _, ok := got.Results[id]; !ok {
			t.Fatalf("lost result %s", id)
		}
	}
	if got.ResultExpiresAt["legacy"].IsZero() {
		t.Fatal("legacy result has no expiry")
	}
}

func TestJournalAckReleasesRemovedBatchReferences(t *testing.T) {
	j := &Journal{pending: []Batch{{BatchID: "one"}, {BatchID: "two"}}}
	j.apply(record{Ack: "one"})
	if j.pending[:cap(j.pending)][1].BatchID != "" {
		t.Fatal("removed batch remains in backing array")
	}
}
