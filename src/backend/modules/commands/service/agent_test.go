package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
)

type fakeAgentStore struct {
	trap     commands.Trap
	command  commands.Command
	dispatch *commands.Dispatch
	recorded commands.AgentResult
	writes   int
}

func (f *fakeAgentStore) Claim(context.Context, string, string, time.Time) (*commands.Dispatch, error) {
	return f.dispatch, nil
}

func (f *fakeAgentStore) ExtendLease(context.Context, string, string, string, string, time.Time) (time.Time, error) {
	return time.Unix(100, 0), nil
}

func (f *fakeAgentStore) RecordResult(ctx context.Context, _, _ string, input commands.AgentResult, _ time.Time, validate func(context.Context, commands.Trap, commands.Command, commands.AgentResult) error) (time.Time, error) {
	if err := validate(ctx, f.trap, f.command, input); err != nil {
		return time.Time{}, err
	}

	f.writes++
	f.recorded = input
	return time.Unix(200, 0), nil
}

func agentContext() context.Context {
	return contract.WithPrincipal(context.Background(), contract.Principal{OrganizationID: contract.ID(orgID), TrapID: contract.ID(trapID), Role: contract.Agent})
}

func TestAgentResultValidation(t *testing.T) {
	revision := int32(2)
	base := commands.AgentResult{
		CommandID: "55555555-5555-4555-8555-555555555555",
		LeaseID:   "77777777-7777-4777-8777-777777777777",
		Status:    commands.Succeeded,
		Result:    json.RawMessage(`{"runtime_state":"stopped","applied_profile_revision":2}`),
		Runtime:   commands.AgentRuntime{RuntimeState: "stopped", AppliedProfileRevision: &revision, BufferState: "ok", BufferCapacityBytes: 1024},
	}

	for _, tt := range []struct {
		name   string
		change func(*commands.AgentResult)
		want   error
	}{
		{"success", func(*commands.AgentResult) {}, nil},
		{"wrong status", func(v *commands.AgentResult) { v.Status = commands.Queued }, commands.ErrResultInvalid},
		{"wrong revision", func(v *commands.AgentResult) { bad := int32(3); v.Runtime.AppliedProfileRevision = &bad }, commands.ErrResultInvalid},
		{"unsafe error code", func(v *commands.AgentResult) {
			v.Status = commands.Failed
			v.Result = nil
			v.Error = &commands.RuntimeError{Code: "internal_path", Message: `C:\secret`}
		}, commands.ErrResultInvalid},
		{"failed safe error", func(v *commands.AgentResult) {
			v.Status = commands.Failed
			v.Result = nil
			v.Error = &commands.RuntimeError{Code: "port_unavailable", Message: `C:\secret`}
		}, nil},
		{"bad buffer", func(v *commands.AgentResult) { v.Runtime.BufferCapacityBytes = 0 }, commands.ErrResultInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeAgentStore{
				trap:    commands.Trap{ID: trapID, OrganizationID: orgID, TypeID: "tcp-banner", TypeVersion: 1},
				command: commands.Command{Action: "apply_config", Status: commands.Running, TargetProfileRevision: &revision},
			}
			agent := NewAgent(store, func(context.Context, string, int32, string, json.RawMessage, bool) error { return nil })
			input := base
			tt.change(&input)
			_, err := agent.RecordResult(agentContext(), orgID, trapID, input)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got error %v, want %v", err, tt.want)
			}

			if tt.want != nil && store.writes != 0 {
				t.Fatal("invalid result was written")
			}

			if tt.name == "failed safe error" && store.recorded.Error.Message != "Port is unavailable" {
				t.Fatalf("unsafe message persisted: %q", store.recorded.Error.Message)
			}
		})
	}
}

func TestAgentOwnership(t *testing.T) {
	store := &fakeAgentStore{dispatch: &commands.Dispatch{CommandID: keyID}}
	agent := NewAgent(store, nil)
	if _, err := agent.Claim(context.Background(), orgID, trapID); err == nil {
		t.Fatal("unauthenticated claim accepted")
	}

	foreign := contract.WithPrincipal(context.Background(), contract.Principal{OrganizationID: contract.ID(orgID), TrapID: contract.ID(keyID), Role: contract.Agent})
	if _, err := agent.Claim(foreign, orgID, trapID); err == nil {
		t.Fatal("foreign trap claimed")
	}

	if _, err := agent.Claim(agentContext(), orgID, trapID); err != nil {
		t.Fatal(err)
	}
}
