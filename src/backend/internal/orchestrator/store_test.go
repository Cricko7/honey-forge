package orchestrator

import (
	"testing"

	"honey-forge/modules/profiles"
)

func TestNextCommandStartsNewTrapOnce(t *testing.T) {
	revision := int32(2)
	active := "command-id"
	for _, tc := range []struct {
		name   string
		target Target
		action string
		params string
	}{
		{"new trap", Target{Snapshot: profiles.Snapshot{ProfileRevision: 2}}, "apply_config", `{"profile_revision":2}`},
		{"config applied", Target{AppliedProfileRevision: &revision}, "start", `{}`},
		{"command in progress", Target{ActiveCommandID: &active}, "", ""},
		{"already started", Target{AppliedProfileRevision: &revision, HasStarted: true}, "", ""},
		{"deleted", Target{Deleted: true}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action, params := nextCommand(tc.target)
			if action != tc.action || string(params) != tc.params {
				t.Fatalf("got %s %s, want %s %s", action, params, tc.action, tc.params)
			}
		})
	}
}
