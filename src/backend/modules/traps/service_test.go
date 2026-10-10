package traps

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"honey-forge/internal/contract"
)

func TestPatchRules(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	name, description := " renamed ", ""
	for _, tt := range []struct {
		name              string
		req               PatchRequest
		revision, version int64
		code              string
		changed           bool
	}{
		{"empty", PatchRequest{}, 1, 1, "validation_failed", false},
		{"no op", PatchRequest{Name: ptr("original")}, 1, math.MaxInt32, "", false},
		{"rename", PatchRequest{Name: &name}, 1, 8, "", true},
		{"clear description", PatchRequest{Description: &description}, 1, 8, "", true},
		{"revision exhausted", PatchRequest{Name: &name}, math.MaxInt32, 1, "revision_exhausted", false},
		{"state exhausted", PatchRequest{Name: &name}, 1, math.MaxInt32, "revision_exhausted", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := Record{Trap: Trap{Name: "original", Description: "old", Revision: tt.revision, StateVersion: tt.version, UpdatedAt: now.Add(-time.Hour)}}
			before := r.Trap
			changed, err := patch(&r, tt.revision, tt.req, now)
			requireCode(t, err, tt.code)
			if changed != tt.changed {
				t.Fatalf("changed=%v", changed)
			}
			if changed && (r.Revision != before.Revision+1 || r.StateVersion != before.StateVersion+1 || !r.UpdatedAt.Equal(now)) {
				t.Fatalf("bad versions: %+v", r.Trap)
			}
			if !changed && r.Trap != before {
				t.Fatal("partial write")
			}
		})
	}
	r := Record{Trap: Trap{Revision: 2, StateVersion: 15}}
	_, err := patch(&r, 1, PatchRequest{Name: &name}, now)
	requireCode(t, err, "revision_mismatch")
}

func TestSafeDeletion(t *testing.T) {
	now := time.Now().UTC()
	online := Record{Trap: Trap{Revision: 1, StateVersion: 9, Connectivity: "online", LastSeenAt: &now, RuntimeState: "stopped", DesiredState: "stopped", Agent: &AgentStatus{BufferState: "ok"}}, Generation: 1, ConnectionID: ptr("connection")}
	for _, tt := range []struct {
		name   string
		change func(*Record)
		code   string
	}{
		{"safe", func(*Record) {}, ""},
		{"active command", func(r *Record) { r.ActiveCommandID = ptr("command") }, "command_in_progress"},
		{"offline", func(r *Record) { r.Connectivity = "offline" }, "trap_offline"},
		{"no connection", func(r *Record) { r.ConnectionID = nil }, "trap_offline"},
		{"stale heartbeat", func(r *Record) { r.LastSeenAt = ptr(now.Add(-31 * time.Second)) }, "trap_offline"},
		{"running", func(r *Record) { r.RuntimeState = "running" }, "trap_not_stopped"},
		{"desired running", func(r *Record) { r.DesiredState = "running" }, "trap_not_stopped"},
		{"events", func(r *Record) { r.Agent.BufferedEvents = 1 }, "trap_buffer_not_empty"},
		{"bytes", func(r *Record) { r.Agent.BufferBytes = 1 }, "trap_buffer_not_empty"},
		{"bad buffer", func(r *Record) { r.Agent.BufferState = "unavailable" }, "trap_buffer_not_empty"},
		{"no agent", func(r *Record) { r.Agent = nil }, "trap_buffer_not_empty"},
		{"ingestion", func(r *Record) { r.PendingIngestions = 1 }, "trap_buffer_not_empty"},
		{"never issued", func(r *Record) {
			r.Generation = 0
			r.Connectivity = "offline"
			r.ConnectionID = nil
			r.RuntimeState = "unknown"
			r.Agent = nil
		}, ""},
		{"new with ingestion", func(r *Record) { r.Generation = 0; r.PendingIngestions = 1 }, "trap_buffer_not_empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := online
			agent := *online.Agent
			r.Agent = &agent
			tt.change(&r)
			requireCode(t, safeDelete(r, 1, now), tt.code)
		})
	}
	requireCode(t, safeDelete(online, 2, now), "revision_mismatch")
}

func TestServiceAuthorization(t *testing.T) {
	s := NewService(nil, nil, "wss://center.example/ws/agent", nil)
	for _, role := range []contract.Role{contract.Viewer, contract.Agent} {
		t.Run(string(role), func(t *testing.T) {
			ctx := contract.WithPrincipal(context.Background(), contract.Principal{Role: role, OrganizationID: contract.NewID()})
			_, err := s.Create(ctx, CreateRequest{})
			requireCode(t, err, "forbidden")
			_, err = s.IssueCredentials(ctx, string(contract.NewID()), 0)
			requireCode(t, err, "forbidden")
			_, err = s.Credentials(ctx, string(contract.NewID()))
			requireCode(t, err, "forbidden")
			requireCode(t, s.Delete(ctx, string(contract.NewID()), 1), "forbidden")
		})
	}
	_, err := s.Read(context.Background(), string(contract.NewID()))
	requireCode(t, err, "unauthenticated")
}

func ptr[T any](v T) *T { return &v }
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var api *contract.Error
	if !errors.As(err, &api) || api.Code != code {
		t.Fatalf("error=%v, want %s", err, code)
	}
}
