package agent

import (
	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
	"path/filepath"
	"testing"
)

func TestRestartRestoresSnapshotRuntimeAndReplaysResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	j, err := OpenJournal(path, "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(j, &fakeRunner{})
	snapshot := demoSnapshot()
	snapshot.ProfileRevision = 2
	apply := configCommand("apply_config", &snapshot)
	if _, err := s.Execute(t.Context(), apply); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(t.Context(), configCommand("start", nil)); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path, "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	runner := &fakeRunner{}
	s = NewService(j, runner)
	if err := s.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if runner.starts != 1 || s.Runtime().RuntimeState != "running" || *s.Runtime().AppliedProfileRevision != 2 {
		t.Fatalf("runtime %+v", s.Runtime())
	}
	old := demoSnapshot()
	if err := s.RestoreWelcome(t.Context(), &old); err != nil {
		t.Fatal(err)
	}
	apply.LeaseID = string(contract.NewID())
	result, err := s.Execute(t.Context(), apply)
	if err != nil || result.LeaseID != apply.LeaseID || result.Status != commands.Succeeded || *result.Runtime.AppliedProfileRevision != 2 || runner.starts != 1 {
		t.Fatalf("replay %+v %v", result, err)
	}
}

func TestInterruptedStopDoesNotRestartListener(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	snapshot := demoSnapshot()
	stop := configCommand("stop", nil)
	if err := j.SaveState(State{Configuration: &snapshot, Running: true, Intent: &stop}); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	s := NewService(j, runner)
	if err := s.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if runner.starts != 0 || s.Runtime().RuntimeState != "stopped" {
		t.Fatal("interrupted stop reopened listener")
	}
	stop.LeaseID = string(contract.NewID())
	if result, err := s.Execute(t.Context(), stop); err != nil || result.Status != commands.Succeeded {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestEmptyJournalWelcomeRestoresOnlyStopped(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	runner := &fakeRunner{}
	s := NewService(j, runner)
	snapshot := demoSnapshot()
	if err := s.RestoreWelcome(t.Context(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if runner.starts != 0 || s.Runtime().RuntimeState != "stopped" || *s.Runtime().AppliedProfileRevision != 1 {
		t.Fatalf("%+v", s.Runtime())
	}
}
