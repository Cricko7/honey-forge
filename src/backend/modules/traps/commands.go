package traps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/commands"
)

// All command lifecycle operations use the same trap row lock as DELETE.
func (r *Repository) Lock(ctx context.Context, tx pgx.Tx, org, id string) (commands.Trap, error) {
	current, err := lockRecord(ctx, tx, org, id)
	if err == nil {
		if identity, connection, ok := agentws.SessionFrom(ctx); ok && (current.Generation != identity.CredentialGeneration || current.ConnectionID == nil || *current.ConnectionID != connection || len(current.TokenHash) == 0) {
			return commands.Trap{}, contract.NewError("agent_unauthenticated")
		}
	}
	var api *contract.Error
	if errors.As(err, &api) && api.Code == "resource_not_found" {
		return commands.Trap{}, commands.ErrNotFound
	}
	return commands.Trap{ID: current.ID, OrganizationID: current.OrganizationID, ProfileID: current.ProfileID, TypeID: current.TypeID, TypeVersion: current.TypeVersion, AppliedProfileRevision: current.AppliedProfileRevision, ActiveCommandID: current.ActiveCommandID}, err
}
func (r *Repository) Activate(ctx context.Context, tx pgx.Tx, trap commands.Trap, command commands.Command) (int64, error) {
	current, err := lockRecord(ctx, tx, trap.OrganizationID, trap.ID)
	if err != nil {
		return 0, err
	}
	version, err := nextVersion(current.StateVersion)
	if err != nil {
		return 0, err
	}
	current.StateVersion = version
	current.ActiveCommandID = &command.ID
	switch command.Action {
	case "start":
		current.DesiredState = "running"
	case "stop":
		current.DesiredState = "stopped"
	case "apply_config":
		current.DesiredProfileRevision = command.TargetProfileRevision
	}
	return version, save(ctx, tx, current)
}
func (r *Repository) Release(ctx context.Context, tx pgx.Tx, trap commands.Trap, command commands.Command) (int64, error) {
	current, err := lockRecord(ctx, tx, trap.OrganizationID, trap.ID)
	if err != nil {
		return 0, err
	}
	if current.ActiveCommandID == nil || *current.ActiveCommandID != command.ID {
		return 0, commands.ErrStaleLease
	}
	version, err := nextVersion(current.StateVersion)
	if err != nil {
		return 0, err
	}
	current.StateVersion = version
	current.ActiveCommandID = nil
	return version, save(ctx, tx, current)
}
func (r *Repository) Complete(ctx context.Context, tx pgx.Tx, trap commands.Trap, command commands.Command, runtime commands.AgentRuntime) (int64, error) {
	current, err := lockRecord(ctx, tx, trap.OrganizationID, trap.ID)
	if err != nil {
		return 0, err
	}
	if current.ActiveCommandID == nil || *current.ActiveCommandID != command.ID {
		return 0, commands.ErrStaleLease
	}
	if command.Status == commands.Running {
		// The stored status was read before RecordResult writes the terminal outcome.
		var status commands.Status
		if err := tx.QueryRow(ctx, `SELECT status FROM commands WHERE id=$1`, command.ID).Scan(&status); err != nil {
			return 0, fmt.Errorf("read terminal outcome: %w", err)
		}
		if status == commands.Succeeded {
			if command.Action == "start" && runtime.RuntimeState != "running" || command.Action == "stop" && runtime.RuntimeState != "stopped" {
				return 0, commands.ErrResultInvalid
			}
			if command.Action == "apply_config" {
				var raw []byte
				if err := tx.QueryRow(ctx, `SELECT configuration FROM commands WHERE id=$1`, command.ID).Scan(&raw); err != nil {
					return 0, fmt.Errorf("read applied snapshot: %w", err)
				}
				if err := json.Unmarshal(raw, &current.AppliedConfiguration); err != nil {
					return 0, fmt.Errorf("decode applied snapshot: %w", err)
				}
			}
		}
	}
	if err := applyRuntime(ctx, tx, &current, runtime); err != nil {
		return 0, err
	}
	version, err := nextVersion(current.StateVersion)
	if err != nil {
		return 0, err
	}
	current.StateVersion = version
	current.ActiveCommandID = nil
	return version, save(ctx, tx, current)
}

// Visible includes tombstones so owner command/event history remains readable.
func (r *Repository) Visible(ctx context.Context, org, id string) (bool, error) {
	if err := readAccess(ctx, org); err != nil {
		return false, err
	}
	var visible bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM traps WHERE organization_id=$1 AND id=$2)`, org, id).Scan(&visible)
	if err != nil {
		return false, fmt.Errorf("check trap ownership: %w", err)
	}
	return visible, nil
}
