package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
)

type agentStore interface {
	Claim(context.Context, string, string, time.Time) (*commands.Dispatch, error)
	ExtendLease(context.Context, string, string, string, string, time.Time) (time.Time, error)
	RecordResult(context.Context, string, string, commands.AgentResult, time.Time, func(context.Context, commands.Trap, commands.Command, commands.AgentResult) error) (time.Time, error)
}

type CheckResult func(context.Context, string, int32, string, json.RawMessage, bool) error

type Agent struct {
	store       agentStore
	checkResult CheckResult
	now         func() time.Time
}

func NewAgent(store agentStore, checkResult CheckResult) *Agent {
	return &Agent{store: store, checkResult: checkResult, now: time.Now}
}

func agentAccess(ctx context.Context, org, trapID string) error {
	if !contract.ValidID(org) || !contract.ValidID(trapID) {
		return commands.ErrNotFound
	}

	if err := contract.RequireAgentTrap(ctx, contract.ID(trapID)); err != nil {
		return err
	}

	p, _ := contract.PrincipalFrom(ctx)
	if string(p.OrganizationID) != org {
		return commands.ErrNotFound
	}

	return nil
}

func (a *Agent) Claim(ctx context.Context, org, trapID string) (*commands.Dispatch, error) {
	if err := agentAccess(ctx, org, trapID); err != nil {
		return nil, err
	}
	if a.store == nil {
		return nil, commands.ErrUnavailable
	}

	return a.store.Claim(ctx, org, trapID, a.now())
}

func (a *Agent) ExtendLease(ctx context.Context, org, trapID, commandID, leaseID string) (time.Time, error) {
	if err := agentAccess(ctx, org, trapID); err != nil {
		return time.Time{}, err
	}

	if !contract.ValidID(commandID) || !contract.ValidID(leaseID) {
		return time.Time{}, commands.ErrStaleLease
	}
	if a.store == nil {
		return time.Time{}, commands.ErrUnavailable
	}

	return a.store.ExtendLease(ctx, org, trapID, commandID, leaseID, a.now())
}

func (a *Agent) RecordResult(ctx context.Context, org, trapID string, input commands.AgentResult) (time.Time, error) {
	if err := agentAccess(ctx, org, trapID); err != nil {
		return time.Time{}, err
	}

	if !contract.ValidID(input.CommandID) || !contract.ValidID(input.LeaseID) || a.checkResult == nil {
		return time.Time{}, commands.ErrResultInvalid
	}
	if a.store == nil {
		return time.Time{}, commands.ErrUnavailable
	}

	if err := validateAgentResult(&input); err != nil {
		return time.Time{}, err
	}

	return a.store.RecordResult(ctx, org, trapID, input, a.now(), func(ctx context.Context, trap commands.Trap, command commands.Command, result commands.AgentResult) error {
		if command.Status != commands.Running {
			return commands.ErrStaleLease
		}

		if result.Status == commands.Succeeded {
			if err := a.checkResult(ctx, trap.TypeID, trap.TypeVersion, command.Action, result.Result, trap.AppliedProfileRevision != nil); err != nil {
				return fmt.Errorf("check command result: %w", err)
			}

			if command.Action == "apply_config" {
				if command.TargetProfileRevision == nil || result.Runtime.AppliedProfileRevision == nil || *command.TargetProfileRevision != *result.Runtime.AppliedProfileRevision {
					return commands.ErrResultInvalid
				}
			} else if !sameRevision(trap.AppliedProfileRevision, result.Runtime.AppliedProfileRevision) {
				return commands.ErrResultInvalid
			}

			var outcome struct {
				Revision     *int32  `json:"applied_profile_revision"`
				RuntimeState *string `json:"runtime_state"`
			}
			if err := json.Unmarshal(result.Result, &outcome); err != nil || !sameRevision(outcome.Revision, result.Runtime.AppliedProfileRevision) {
				return commands.ErrResultInvalid
			}
			if outcome.RuntimeState != nil && *outcome.RuntimeState != result.Runtime.RuntimeState {
				return commands.ErrResultInvalid
			}
		}

		return nil
	})
}

func validateAgentResult(input *commands.AgentResult) error {
	if input.Status != commands.Succeeded && input.Status != commands.Failed {
		return commands.ErrResultInvalid
	}

	if input.Runtime.BufferedEvents < 0 || input.Runtime.BufferBytes < 0 || input.Runtime.BufferCapacityBytes <= 0 {
		return commands.ErrResultInvalid
	}

	switch input.Runtime.RuntimeState {
	case "running", "stopped", "error", "unknown":
	default:
		return commands.ErrResultInvalid
	}

	switch input.Runtime.BufferState {
	case "ok", "full", "unavailable":
	default:
		return commands.ErrResultInvalid
	}

	if input.Runtime.RuntimeState == "error" && input.Runtime.LastError == nil {
		return commands.ErrResultInvalid
	}

	if input.Runtime.AppliedProfileRevision != nil && *input.Runtime.AppliedProfileRevision < 1 {
		return commands.ErrResultInvalid
	}

	if input.Runtime.LastError != nil {
		if err := commands.NormalizeRuntimeError(input.Runtime.LastError); err != nil {
			return err
		}
	}

	if input.Status == commands.Succeeded {
		var result map[string]json.RawMessage
		if input.Error != nil || contract.CheckJSON(input.Result) != nil || json.Unmarshal(input.Result, &result) != nil || result == nil {
			return commands.ErrResultInvalid
		}

		return nil
	}

	if len(input.Result) != 0 || input.Error == nil {
		return commands.ErrResultInvalid
	}

	return commands.NormalizeRuntimeError(input.Error)
}

func sameRevision(a, b *int32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
}
