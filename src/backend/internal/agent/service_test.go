package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

type fakeRunner struct {
	starts, stops int
	failAt        int
}

func (r *fakeRunner) Start(context.Context, profiles.Snapshot) error {
	r.starts++
	if r.starts == r.failAt {
		return fmt.Errorf("port unavailable")
	}
	return nil
}
func (r *fakeRunner) Stop() error { r.stops++; return nil }

func TestApplyConfigRollsBackWhenNewListenerCannotStart(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	r := &fakeRunner{failAt: 2}
	s := NewService(j, r)
	dispatch := func(action string, config *profiles.Snapshot) commands.Dispatch {
		return commands.Dispatch{CommandID: string(contract.NewID()), LeaseID: string(contract.NewID()), Action: action, Configuration: config, ExpiresAt: time.Now().Add(time.Minute)}
	}
	old := demoSnapshot()
	if _, err := s.Execute(t.Context(), dispatch("apply_config", &old)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(t.Context(), dispatch("start", nil)); err != nil {
		t.Fatal(err)
	}
	next := demoSnapshot()
	next.ProfileRevision = 2
	result, err := s.Execute(t.Context(), dispatch("apply_config", &next))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != commands.Failed || result.Runtime.RuntimeState != "running" || *result.Runtime.AppliedProfileRevision != 1 || r.starts != 3 {
		t.Fatalf("rollback: %+v starts=%d", result, r.starts)
	}
}

func demoSnapshot() profiles.Snapshot {
	return profiles.Snapshot{TypeID: "tcp-banner", TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{"listeners": []any{map[string]any{"name": "demo", "port": 2222, "banner": "hello", "close_after_banner": true}}, "logging": map[string]any{"capture_payload": false, "max_payload_bytes": 0}, "management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100}}}
}

func TestCommandsCreateConfigureStartStopAndReplay(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	runner := &fakeRunner{}
	s := NewService(j, runner)
	command := func(action string, config *profiles.Snapshot) commands.Dispatch {
		return commands.Dispatch{CommandID: string(contract.NewID()), LeaseID: string(contract.NewID()), Action: action, Configuration: config, Params: json.RawMessage(`{}`), ExpiresAt: time.Now().Add(time.Minute)}
	}
	if got, err := s.Execute(t.Context(), command("start", nil)); err != nil || got.Status != commands.Failed {
		t.Fatalf("start without config: %v %v", got, err)
	}
	snapshot := demoSnapshot()
	if got, err := s.Execute(t.Context(), command("apply_config", &snapshot)); err != nil || got.Status != commands.Succeeded || got.Runtime.RuntimeState != "stopped" {
		t.Fatalf("apply: %v %v", got, err)
	}
	start := command("start", nil)
	if got, err := s.Execute(t.Context(), start); err != nil || got.Status != commands.Succeeded || got.Runtime.RuntimeState != "running" {
		t.Fatalf("start: %v %v", got, err)
	}
	start.LeaseID = string(contract.NewID())
	if got, err := s.Execute(t.Context(), start); err != nil || got.LeaseID != start.LeaseID || runner.starts != 1 {
		t.Fatalf("replay: %v %v", got, err)
	}
	if got, err := s.Execute(t.Context(), command("stop", nil)); err != nil || got.Status != commands.Succeeded || got.Runtime.RuntimeState != "stopped" {
		t.Fatalf("stop: %v %v", got, err)
	}
	if runner.stops != 1 {
		t.Fatal(runner)
	}
}

func TestExpiredCommandHasNoSideEffects(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	r := &fakeRunner{}
	s := NewService(j, r)
	if _, err := s.Execute(t.Context(), commands.Dispatch{CommandID: string(contract.NewID()), Action: "start", ExpiresAt: time.Now().Add(-time.Second)}); err == nil || r.starts != 0 {
		t.Fatal("expired command executed")
	}
}
