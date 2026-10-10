package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

const (
	userID = "11111111-1111-4111-8111-111111111111"
	orgID  = "22222222-2222-4222-8222-222222222222"
	trapID = "33333333-3333-4333-8333-333333333333"
	keyID  = "44444444-4444-4444-8444-444444444444"
)

type fakeStore struct {
	trap       commands.Trap
	command    commands.Command
	normalized json.RawMessage
	created    int
	err        error
}

func (f *fakeStore) Read(_ context.Context, _, _, id string) (commands.Command, error) {
	if f.command.ID != id {
		return commands.Command{}, commands.ErrNotFound
	}

	return f.command, nil
}

func (f *fakeStore) List(_ context.Context, _, _ string, _ commands.ListQuery) ([]commands.Command, bool, error) {
	if f.command.ID == "" {
		return []commands.Command{}, false, nil
	}

	return []commands.Command{f.command}, false, nil
}

func (f *fakeStore) Create(ctx context.Context, org, trapID, requestID string, normalized json.RawMessage, prepare func(context.Context, commands.Trap, commands.SnapshotReader) (commands.Prepared, error)) (commands.CreateResult, error) {
	if f.err != nil {
		return commands.CreateResult{}, f.err
	}

	if f.command.ID != "" {
		if f.command.RequestID == requestID {
			if string(f.normalized) != string(normalized) {
				return commands.CreateResult{}, commands.ErrIdempotencyConflict
			}

			return commands.CreateResult{Command: f.command, Replayed: true}, nil
		}

		return commands.CreateResult{}, commands.ErrInProgress
	}

	prepared, err := prepare(ctx, f.trap, fakeSnapshot{})
	if err != nil {
		return commands.CreateResult{}, err
	}

	f.created++
	f.normalized = append([]byte(nil), normalized...)
	f.command = commands.Command{ID: "55555555-5555-4555-8555-555555555555", TrapID: trapID, RequestID: requestID, Action: prepared.Action, Params: prepared.Params, Status: commands.Queued, TargetProfileRevision: prepared.TargetProfileRevision}
	return commands.CreateResult{Command: f.command}, nil
}

type fakeSnapshot struct{}

func (fakeSnapshot) Current(context.Context, string, string, int32) (profiles.Snapshot, error) {
	return profiles.Snapshot{ProfileID: "66666666-6666-4666-8666-666666666666", ProfileRevision: 2, TypeID: "tcp-banner", TypeVersion: 1, Config: profiles.Object{"listen_port": 2222}}, nil
}

func request(action, params string) commands.CreateRequest {
	return commands.CreateRequest{RequestID: keyID, Action: action, Params: json.RawMessage(params)}
}

func admin() auth.AuthContext {
	return auth.AuthContext{UserID: userID, OrganizationID: orgID, Role: auth.RoleAdmin}
}

func TestCreateRules(t *testing.T) {
	tests := []struct {
		name        string
		trap        commands.Trap
		req         commands.CreateRequest
		role        string
		want        error
		wantCreated int
	}{
		{"stop without config", commands.Trap{ID: trapID, OrganizationID: orgID, TypeID: "tcp-banner", TypeVersion: 1}, request("stop", `{}`), "admin", nil, 1},
		{"start without config", commands.Trap{ID: trapID, OrganizationID: orgID, TypeID: "tcp-banner", TypeVersion: 1}, request("start", `{}`), "admin", commands.ErrConfigurationNotApplied, 0},
		{"apply current revision", commands.Trap{ID: trapID, OrganizationID: orgID, ProfileID: "66666666-6666-4666-8666-666666666666", TypeID: "tcp-banner", TypeVersion: 1}, request("apply_config", `{"profile_revision":2}`), "admin", nil, 1},
		{"apply changed profile", commands.Trap{ID: trapID, OrganizationID: orgID, ProfileID: "66666666-6666-4666-8666-666666666666", TypeID: "tcp-banner", TypeVersion: 1}, request("apply_config", `{"profile_revision":1}`), "admin", commands.ErrProfileChanged, 0},
		{"unsupported action", commands.Trap{ID: trapID, OrganizationID: orgID, TypeID: "tcp-banner", TypeVersion: 1}, request("restore", `{}`), "admin", commands.ErrUnsupportedAction, 0},
		{"invalid params", commands.Trap{ID: trapID, OrganizationID: orgID, TypeID: "tcp-banner", TypeVersion: 1}, request("stop", `{"extra":true}`), "admin", commands.ErrInvalidParams, 0},
		{"viewer", commands.Trap{ID: trapID, OrganizationID: orgID, TypeID: "tcp-banner", TypeVersion: 1}, request("stop", `{}`), "viewer", commands.ErrForbidden, 0},
		{"foreign trap", commands.Trap{ID: trapID, OrganizationID: "77777777-7777-4777-8777-777777777777", TypeID: "tcp-banner", TypeVersion: 1}, request("stop", `{}`), "admin", commands.ErrNotFound, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{trap: tt.trap}
			service := New(store, func(_ context.Context, _ string, _ int32, action string, params json.RawMessage) error {
				if action == "restore" {
					return commands.ErrUnsupportedAction
				}

				if action == "stop" && string(params) != `{}` {
					return commands.ErrInvalidParams
				}

				return nil
			})

			a := admin()
			if tt.role == "viewer" {
				a.Role = auth.RoleViewer
			}

			_, err := service.Create(t.Context(), a, trapID, tt.req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got error %v, want %v", err, tt.want)
			}

			if store.created != tt.wantCreated {
				t.Fatalf("created %d commands, want %d", store.created, tt.wantCreated)
			}
		})
	}
}

func TestReplayBeforeActiveConflict(t *testing.T) {
	store := &fakeStore{trap: commands.Trap{ID: trapID, OrganizationID: orgID, TypeID: "tcp-banner", TypeVersion: 1}}
	service := New(store, func(context.Context, string, int32, string, json.RawMessage) error { return nil })

	first, err := service.Create(t.Context(), admin(), trapID, request("stop", `{}`))
	if err != nil {
		t.Fatal(err)
	}

	second, err := service.Create(t.Context(), admin(), trapID, request("stop", `{}`))
	if err != nil || !second.Replayed || second.Command.ID != first.Command.ID {
		t.Fatalf("replay: %+v, %v", second, err)
	}

	if _, err := service.Create(t.Context(), admin(), trapID, request("start", `{}`)); !errors.Is(err, commands.ErrIdempotencyConflict) {
		t.Fatalf("changed request content: %v", err)
	}

	other := request("start", `{}`)
	other.RequestID = "88888888-8888-4888-8888-888888888888"
	if _, err := service.Create(t.Context(), admin(), trapID, other); !errors.Is(err, commands.ErrInProgress) {
		t.Fatalf("active command: %v", err)
	}
}
