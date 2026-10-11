package repository

import (
	"testing"
	"time"

	"honey-forge/modules/commands"
)

func TestExpiryReason(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	seen := now.Add(-time.Hour)
	for _, tc := range []struct {
		name         string
		status       commands.Status
		connectivity string
		seen         *time.Time
		age          time.Duration
		want         string
	}{
		{"old never connected queue", commands.Queued, "offline", nil, 5 * time.Minute, "agent_not_connected"},
		{"new queue", commands.Queued, "offline", nil, 5*time.Minute - time.Nanosecond, ""},
		{"connected before", commands.Queued, "offline", &seen, 10 * time.Minute, ""},
		{"online", commands.Queued, "online", nil, 10 * time.Minute, ""},
		{"running", commands.Running, "offline", nil, 10 * time.Minute, ""},
		{"normal deadline", commands.Queued, "offline", nil, 24 * time.Hour, "command_expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := commands.Command{Status: tc.status, CreatedAt: now.Add(-tc.age), ExpiresAt: now.Add(24*time.Hour - tc.age)}
			trap := commands.Trap{Connectivity: tc.connectivity, LastSeenAt: tc.seen}
			if got := expiryReason(command, trap, now); got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}
