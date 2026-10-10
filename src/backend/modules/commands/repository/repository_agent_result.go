package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/mutation"
	"honey-forge/modules/commands"
)

// RecordResult calls validate while holding the command and trap locks. A
// repeated terminal result is acknowledged without changing the stored runtime.
func (r *Repository) RecordResult(ctx context.Context, org, trapID string, input commands.AgentResult, now time.Time, validate func(context.Context, commands.Trap, commands.Command, commands.AgentResult) error) (time.Time, error) {
	// PostgreSQL persists timestamps at microsecond precision. The first ack
	// must use the same UTC representation as a replay read from storage.
	now = now.UTC().Truncate(time.Microsecond)
	if validate == nil {
		return time.Time{}, commands.ErrResultInvalid
	}
	if err := agentAccess(ctx, org, trapID); err != nil {
		return time.Time{}, err
	}
	if err := r.ExpireDue(ctx, org, trapID, now); err != nil {
		return time.Time{}, fmt.Errorf("expire command before result: %w", err)
	}

	var recordedAt time.Time
	err := r.agentTransaction(ctx, org, trapID, func(tx pgx.Tx, trap commands.Trap) error {
		command, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM commands WHERE organization_id=$1 AND trap_id=$2 AND id=$3 FOR UPDATE`, org, trapID, input.CommandID))
		if err != nil {
			return fmt.Errorf("read result command: %w", err)
		}

		if command.Status == commands.Succeeded || command.Status == commands.Failed {
			if command.Status != input.Status || !sameJSON(command.Result, input.Result) || !sameError(command.Error, input.Error) {
				return commands.ErrResultConflict
			}

			recordedAt = *command.FinishedAt
			return nil
		}

		if command.Status == commands.Expired || !now.Before(command.ExpiresAt) {
			return commands.ErrExpired
		}

		if trap.ActiveCommandID == nil || *trap.ActiveCommandID != command.ID {
			return commands.ErrStaleLease
		}

		var lease *string
		var leaseEnd *time.Time
		if err := tx.QueryRow(ctx, `SELECT lease_id::text,lease_expires_at FROM commands WHERE id=$1`, command.ID).Scan(&lease, &leaseEnd); err != nil {
			return fmt.Errorf("read command lease: %w", err)
		}

		if command.Status != commands.Running || lease == nil || *lease != input.LeaseID || leaseEnd == nil || now.After(*leaseEnd) {
			return commands.ErrStaleLease
		}

		if err := validate(ctx, trap, command, input); err != nil {
			return err
		}

		var result, runtimeError any
		if input.Status == commands.Succeeded {
			result = string(input.Result)
		} else {
			encoded, err := json.Marshal(input.Error)
			if err != nil {
				return fmt.Errorf("encode runtime error: %w", err)
			}

			runtimeError = string(encoded)
		}

		if _, err := tx.Exec(ctx, `UPDATE commands SET status=$2,result=$3::jsonb,error=$4::jsonb,finished_at=$5,lease_id=NULL,lease_expires_at=NULL WHERE id=$1`, command.ID, input.Status, result, runtimeError, now.UTC()); err != nil {
			return fmt.Errorf("record command result: %w", err)
		}

		stateVersion, err := r.traps.Complete(ctx, tx, trap, command, input.Runtime)
		if err != nil {
			return fmt.Errorf("complete trap command: %w", err)
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

		recordedAt = now.UTC()
		return nil
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("record command result: %w", err)
	}

	return recordedAt, nil
}

func sameJSON(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}

	left, leftErr := mutation.Fingerprint(a)
	right, rightErr := mutation.Fingerprint(b)
	return leftErr == nil && rightErr == nil && left == right
}

func sameError(a, b *commands.RuntimeError) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return a.Code == b.Code && a.Message == b.Message
}
