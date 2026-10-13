package agent

import (
	"path/filepath"
	"testing"

	"honey-forge/modules/commands"
)

func TestHostTelemetryStartFailureUsesRuntimeStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int32
		code    string
	}{
		{name: "existing SSH", version: 1, code: "port_unavailable"},
		{name: "SSH sensor dependency", version: 2, code: "runtime_start_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			journal, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := journal.Close(); err != nil {
					t.Error(err)
				}
			}()
			snapshot := demoSnapshot()
			snapshot.TypeID = "ssh-high-interaction"
			snapshot.TypeVersion = tc.version
			snapshot.Config = map[string]any{
				"services":   []any{map[string]any{"name": "ssh", "port": 2222, "username": "root", "password": "bait"}},
				"session":    map[string]any{"max_seconds": 300, "max_connections": 4, "max_recording_bytes": 1048576, "max_upload_bytes": 1048576},
				"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
			}
			service := NewService(journal, &fakeRunner{failAt: 1})
			if result, err := service.Execute(t.Context(), configCommand("apply_config", &snapshot)); err != nil || result.Status != commands.Succeeded {
				t.Fatalf("configure: %+v %v", result, err)
			}
			result, err := service.Execute(t.Context(), configCommand("start", nil))
			if err != nil || result.Status != commands.Failed || result.Error == nil || result.Error.Code != tc.code {
				t.Fatalf("start: %+v %v", result, err)
			}
		})
	}
}
