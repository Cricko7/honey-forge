package agent

import (
	"honey-forge/modules/commands"
	"path/filepath"
	"testing"
)

func TestCommandAckDoesNotEraseReplayOutcome(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	runner := &fakeRunner{}
	s := NewService(j, runner)
	snapshot := demoSnapshot()
	dispatch := configCommand("apply_config", &snapshot)
	if _, err := s.Execute(t.Context(), dispatch); err != nil {
		t.Fatal(err)
	}
	if len(s.PendingResults()) != 1 {
		t.Fatal("unacknowledged result not pending")
	}
	if err := s.AckResult(dispatch.CommandID, commands.Failed); err == nil || len(s.PendingResults()) != 1 {
		t.Fatal("mismatched ack removed result")
	}
	if err := s.AckResult(dispatch.CommandID, commands.Succeeded); err != nil {
		t.Fatal(err)
	}
	if len(s.PendingResults()) != 0 {
		t.Fatal("acked result remains pending")
	}
	if result, err := s.Execute(t.Context(), dispatch); err != nil || result.Status != commands.Succeeded || runner.starts != 0 {
		t.Fatalf("replay %+v %v", result, err)
	}
}
