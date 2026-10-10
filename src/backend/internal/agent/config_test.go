package agent

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

func configCommand(action string, snapshot *profiles.Snapshot) commands.Dispatch {
	return commands.Dispatch{CommandID: string(contract.NewID()), LeaseID: string(contract.NewID()), Action: action, Configuration: snapshot, Params: json.RawMessage(`{}`), ExpiresAt: time.Now().Add(time.Minute)}
}

func TestFlushIntervalFollowsAppliedConfiguration(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	runner := &fakeRunner{}
	s := NewService(j, runner)
	if got := s.FlushInterval(); got != 100*time.Millisecond {
		t.Fatalf("unconfigured interval = %v", got)
	}
	first := demoSnapshot()
	first.Config["management"].(map[string]any)["telemetry_flush_interval_ms"] = 400
	if result, err := s.Execute(t.Context(), configCommand("apply_config", &first)); err != nil || result.Status != commands.Succeeded {
		t.Fatalf("apply first: %+v %v", result, err)
	}
	if result, err := s.Execute(t.Context(), configCommand("start", nil)); err != nil || result.Status != commands.Succeeded {
		t.Fatalf("start: %+v %v", result, err)
	}
	if got := s.FlushInterval(); got != 400*time.Millisecond {
		t.Fatalf("first interval = %v", got)
	}
	second := demoSnapshot()
	second.ProfileRevision = 2
	second.Config["management"].(map[string]any)["telemetry_flush_interval_ms"] = 900
	update := configCommand("apply_config", &second)
	if result, err := s.Execute(t.Context(), update); err != nil || result.Status != commands.Succeeded {
		t.Fatalf("apply running trap: %+v %v", result, err)
	}
	if got := s.FlushInterval(); got != 900*time.Millisecond {
		t.Fatalf("updated interval = %v", got)
	}
	starts, stops := runner.starts, runner.stops
	update.LeaseID = string(contract.NewID())
	if result, err := s.Execute(t.Context(), update); err != nil || result.Status != commands.Succeeded || runner.starts != starts || runner.stops != stops {
		t.Fatalf("replayed config restarted trap: %+v %v starts=%d stops=%d", result, err, runner.starts, runner.stops)
	}
	invalid := demoSnapshot()
	invalid.ProfileRevision = 3
	invalid.Config["management"].(map[string]any)["telemetry_flush_interval_ms"] = 1001
	if result, err := s.Execute(t.Context(), configCommand("apply_config", &invalid)); err != nil || result.Status != commands.Failed {
		t.Fatalf("invalid apply: %+v %v", result, err)
	}
	if got := s.FlushInterval(); got != 900*time.Millisecond {
		t.Fatalf("failed apply changed interval to %v", got)
	}
}

func TestChildBootstrapUsesPipe(t *testing.T) {
	want := childBootstrap{URL: "ws://127.0.0.1:1234/trap-stream", Token: "private", Snapshot: demoSnapshot()}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readChildBootstrap(bytes.NewReader(append(raw, '\n')))
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != want.URL || got.Token != want.Token || got.Snapshot.ProfileRevision != want.Snapshot.ProfileRevision {
		t.Fatalf("bootstrap mismatch: %+v", got)
	}
}
