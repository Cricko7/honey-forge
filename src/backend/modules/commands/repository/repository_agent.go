package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

func agentAccess(ctx context.Context, org, trapID string) error {
	if err := contract.RequireAgentTrap(ctx, contract.ID(trapID)); err != nil {
		return err
	}

	p, _ := contract.PrincipalFrom(ctx)
	if string(p.OrganizationID) != org {
		return commands.ErrNotFound
	}

	return nil
}

func (r *Repository) agentTransaction(ctx context.Context, org, trapID string, fn func(pgx.Tx, commands.Trap) error) (err error) {
	if err := agentAccess(ctx, org, trapID); err != nil {
		return err
	}

	if r.traps == nil {
		return commands.ErrUnavailable
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin agent command transaction: %w", err)
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if e := tx.Rollback(rollback); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback agent command: %w", e))
		}
	}()

	if _, err := tx.Exec(ctx, `INSERT INTO organization_changes(organization_id) VALUES($1) ON CONFLICT DO NOTHING`, org); err != nil {
		return fmt.Errorf("initialize change journal: %w", err)
	}

	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT sequence FROM organization_changes WHERE organization_id=$1 FOR UPDATE`, org).Scan(&sequence); err != nil {
		return fmt.Errorf("lock change journal: %w", err)
	}

	trap, err := r.traps.Lock(ctx, tx, org, trapID)
	if err != nil {
		return fmt.Errorf("lock trap for agent command: %w", err)
	}

	if trap.ID != trapID || trap.OrganizationID != org {
		return commands.ErrNotFound
	}

	if err := fn(tx, trap); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent command: %w", err)
	}

	return nil
}

// Claim fences an earlier connection with a new lease. Module 08 authenticates
// the current connection and checks its supported type and action before calling.
func (r *Repository) Claim(ctx context.Context, org, trapID string, now time.Time) (*commands.Dispatch, error) {
	if err := agentAccess(ctx, org, trapID); err != nil {
		return nil, err
	}

	if err := r.ExpireDue(ctx, org, trapID, now); err != nil {
		return nil, err
	}

	var dispatch *commands.Dispatch
	err := r.agentTransaction(ctx, org, trapID, func(tx pgx.Tx, trap commands.Trap) error {
		if trap.ActiveCommandID == nil {
			return nil
		}

		var command commands.Command
		var configuration *string
		var err error
		command, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM commands WHERE organization_id=$1 AND trap_id=$2 AND id=$3 FOR UPDATE`, org, trapID, *trap.ActiveCommandID))
		if err != nil {
			return fmt.Errorf("read dispatch command: %w", err)
		}

		if command.Status != commands.Queued && command.Status != commands.Running {
			return fmt.Errorf("active command is terminal")
		}

		if !now.Before(command.ExpiresAt) {
			return commands.ErrExpired
		}

		if err := tx.QueryRow(ctx, `SELECT configuration::text FROM commands WHERE id=$1`, command.ID).Scan(&configuration); err != nil {
			return fmt.Errorf("read pinned configuration: %w", err)
		}

		leaseID := string(contract.NewID())
		leaseEnd := now.Add(time.Minute)
		if leaseEnd.After(command.ExpiresAt) {
			leaseEnd = command.ExpiresAt
		}

		if _, err := tx.Exec(ctx, `UPDATE commands SET status='running',started_at=COALESCE(started_at,$2),lease_id=$3,lease_expires_at=$4 WHERE id=$1`, command.ID, now.UTC(), leaseID, leaseEnd.UTC()); err != nil {
			return fmt.Errorf("claim command: %w", err)
		}

		dispatch = &commands.Dispatch{CommandID: command.ID, LeaseID: leaseID, LeaseExpiresAt: leaseEnd.UTC(), Action: command.Action, Params: command.Params, ExpiresAt: command.ExpiresAt}
		if configuration != nil {
			var snapshot profiles.Snapshot
			if err := json.Unmarshal([]byte(*configuration), &snapshot); err != nil {
				return fmt.Errorf("decode pinned configuration: %w", err)
			}

			dispatch.Configuration = &snapshot
		}

		return appendChange(ctx, tx, org, "command.changed", command.ID, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("claim command: %w", err)
	}

	return dispatch, nil
}

func (r *Repository) ExtendLease(ctx context.Context, org, trapID, commandID, leaseID string, now time.Time) (time.Time, error) {
	if err := agentAccess(ctx, org, trapID); err != nil {
		return time.Time{}, err
	}

	var expires time.Time
	err := r.pool.QueryRow(ctx, `UPDATE commands SET lease_expires_at=LEAST(expires_at,$5::timestamptz + interval '60 seconds') WHERE organization_id=$1 AND trap_id=$2 AND id=$3 AND lease_id=$4 AND status='running' AND expires_at>$5 AND lease_expires_at>=$5 RETURNING lease_expires_at`, org, trapID, commandID, leaseID, now.UTC()).Scan(&expires)
	if errors.Is(err, pgx.ErrNoRows) {
		var deadline time.Time
		var status commands.Status
		lookup := r.pool.QueryRow(ctx, `SELECT expires_at,status FROM commands WHERE organization_id=$1 AND trap_id=$2 AND id=$3`, org, trapID, commandID).Scan(&deadline, &status)
		if lookup == nil && (status == commands.Expired || !now.Before(deadline)) {
			return time.Time{}, commands.ErrExpired
		}
		if lookup != nil && !errors.Is(lookup, pgx.ErrNoRows) {
			return time.Time{}, fmt.Errorf("check command deadline: %w", lookup)
		}

		return time.Time{}, commands.ErrStaleLease
	}

	if err != nil {
		return time.Time{}, fmt.Errorf("extend command lease: %w", err)
	}

	return expires.UTC(), nil
}
