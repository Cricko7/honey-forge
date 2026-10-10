package traps

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/modules/profiles"
	profilerepo "honey-forge/modules/profiles/repository"
)

func (r *Repository) Create(ctx context.Context, org string, req CreateRequest, check func(context.Context, profiles.Profile) error) (CreateResult, error) {
	encoded, err := json.Marshal(req)
	if err != nil {
		return CreateResult{}, fmt.Errorf("encode trap request: %w", err)
	}
	var created Record
	result, err := r.mutations.Create(ctx, mutation.Scope{OrganizationID: contract.ID(org), Route: "/api/traps", RequestID: contract.ID(req.RequestID)}, encoded, func(ctx context.Context, tx pgx.Tx) (mutation.Outcome, error) {
		p, err := profilerepo.ReadProfileForBinding(ctx, tx, org, req.ProfileID)
		if err != nil {
			return mutation.Outcome{}, profileError(err)
		}
		if err := check(ctx, p); err != nil {
			return mutation.Outcome{}, err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		created = Record{OrganizationID: org, Trap: Trap{ID: string(contract.NewID()), Name: req.Name, Description: req.Description, ProfileID: p.ID, TypeID: p.TypeID, TypeVersion: p.TypeVersion, InteractionLevel: p.InteractionLevel, Revision: 1, StateVersion: 1, CreatedAt: now, UpdatedAt: now, Connectivity: "offline", RuntimeState: "unknown", DesiredState: "stopped"}}
		initial, err := json.Marshal(created.Trap)
		if err != nil {
			return mutation.Outcome{}, fmt.Errorf("encode initial trap: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO traps(id,organization_id,profile_id,type_id,type_version,interaction_level,name,description,created_at,updated_at,initial_trap) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$10::jsonb)`, created.ID, org, p.ID, p.TypeID, p.TypeVersion, p.InteractionLevel, req.Name, req.Description, now, string(initial))
		if err != nil {
			return mutation.Outcome{}, fmt.Errorf("insert trap: %w", err)
		}
		return outcome(created, "trap.created"), nil
	})
	if err != nil {
		return CreateResult{}, err
	}
	if result.Replayed {
		var initial []byte
		if err := r.pool.QueryRow(ctx, `SELECT initial_trap FROM traps WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL`, org, string(result.ResourceID)).Scan(&initial); err != nil {
			if err == pgx.ErrNoRows {
				return CreateResult{}, contract.NewError("request_already_used")
			}
			return CreateResult{}, fmt.Errorf("read initial trap: %w", err)
		}
		if err := json.Unmarshal(initial, &created.Trap); err != nil {
			return CreateResult{}, fmt.Errorf("decode initial trap: %w", err)
		}
	}
	return CreateResult{Trap: created.Trap, Replayed: result.Replayed}, nil
}
func (r *Repository) Delete(ctx context.Context, org, id string, check func(Record) error) error {
	return r.mutations.Delete(ctx, contract.ID(org), contract.ID(id), func(ctx context.Context, tx pgx.Tx) (mutation.Outcome, error) {
		current, err := lockRecord(ctx, tx, org, id)
		if err != nil {
			return mutation.Outcome{}, err
		}
		// The active pointer and the unique active-command index must agree.
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM commands WHERE trap_id=$1 AND status IN ('queued','running'))`, id).Scan(&active); err != nil {
			return mutation.Outcome{}, fmt.Errorf("check active commands: %w", err)
		}
		if active {
			return mutation.Outcome{}, contract.NewError("command_in_progress")
		}
		if err := check(current); err != nil {
			return mutation.Outcome{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE traps SET deleted_at=clock_timestamp(),token_hash=NULL,connection_id=NULL,connectivity='offline' WHERE organization_id=$1 AND id=$2`, org, id); err != nil {
			return mutation.Outcome{}, fmt.Errorf("tombstone trap: %w", err)
		}
		return outcome(current, "trap.deleted"), nil
	})
}
