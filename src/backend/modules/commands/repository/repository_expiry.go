package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"honey-forge/modules/commands"
)

// ExpireDue is also the entry point for module 08's expiry scheduler.
func (r *Repository) ExpireDue(ctx context.Context, org, trapID string, now time.Time) (err error) {
	if r.traps == nil {
		return commands.ErrUnavailable
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin command expiry: %w", err)
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if e := tx.Rollback(rollback); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback command expiry: %w", e))
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
		return fmt.Errorf("lock trap for command expiry: %w", err)
	}

	if trap.OrganizationID != org || trap.ID != trapID {
		return commands.ErrNotFound
	}

	if trap.ActiveCommandID != nil {
		command, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM commands WHERE organization_id=$1 AND trap_id=$2 AND id=$3 FOR UPDATE`, org, trapID, *trap.ActiveCommandID))
		if err != nil {
			return fmt.Errorf("read active command: %w", err)
		}

		if now.Before(command.ExpiresAt) {
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("commit command expiry check: %w", err)
			}

			return nil
		}

		if command.Status != commands.Queued && command.Status != commands.Running {
			return fmt.Errorf("trap references terminal command")
		}

		failure, err := json.Marshal(commands.RuntimeError{Code: "command_expired", Message: "Command has expired"})
		if err != nil {
			return fmt.Errorf("encode expiry error: %w", err)
		}

		if _, err := tx.Exec(ctx, `UPDATE commands SET status='expired',finished_at=$2,error=$3::jsonb,lease_id=NULL,lease_expires_at=NULL WHERE id=$1`, command.ID, now.UTC(), string(failure)); err != nil {
			return fmt.Errorf("expire command: %w", err)
		}

		stateVersion, err := r.traps.Release(ctx, tx, trap, command)
		if err != nil {
			return fmt.Errorf("release expired command: %w", err)
		}

		if err := appendChange(ctx, tx, org, "command.changed", command.ID, nil); err != nil {
			return err
		}

		metadata, err := json.Marshal(struct {
			StateVersion int64 `json:"state_version"`
		}{stateVersion})
		if err != nil {
			return fmt.Errorf("encode trap change: %w", err)
		}

		if err := appendChange(ctx, tx, org, "trap.changed", trapID, metadata); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit command expiry: %w", err)
	}

	return nil
}

func appendChange(ctx context.Context, tx pgx.Tx, org, kind, resourceID string, metadata json.RawMessage) error {
	if metadata == nil {
		metadata = json.RawMessage(`{}`)
	}

	var sequence int64
	if err := tx.QueryRow(ctx, `UPDATE organization_changes SET sequence=sequence+1 WHERE organization_id=$1 RETURNING sequence`, org).Scan(&sequence); err != nil {
		return fmt.Errorf("advance change journal: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO mutation_changes(organization_id,sequence,type,resource_id,metadata) VALUES($1,$2,$3,$4,$5::jsonb)`, org, sequence, kind, resourceID, string(metadata)); err != nil {
		return fmt.Errorf("record command change: %w", err)
	}

	return nil
}
