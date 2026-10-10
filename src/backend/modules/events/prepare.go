package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/modules/agentws"
	"honey-forge/modules/profiles"
)

func batchFingerprint(events []AgentEvent) ([32]byte, error) {
	raw, err := json.Marshal(struct {
		Events []AgentEvent `json:"events"`
	}{events})
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode batch: %w", err)
	}
	return mutation.Fingerprint(raw)
}

func issuedSnapshot(ctx context.Context, tx pgx.Tx, trapID string, revision int64) (profiles.Snapshot, error) {
	var snapshot profiles.Snapshot
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT configuration FROM commands WHERE trap_id=$1 AND action='apply_config' AND target_profile_revision=$2 AND started_at IS NOT NULL ORDER BY created_at DESC LIMIT 1`, trapID, revision).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, contract.NewError("telemetry_invalid")
	}
	if err != nil {
		return snapshot, fmt.Errorf("read event snapshot: %w", err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode event snapshot: %w", err)
	}
	return snapshot, nil
}

// Prepare pins the normalized batch before any broker side effect. A failed
// publish or materialization cannot permit new content under the same batch ID.
func (r *Repository) Prepare(ctx context.Context, identity agentws.Identity, connection, batchID string, events []AgentEvent) error {
	hash, err := batchFingerprint(events)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		trap, err := r.traps.IngestionTrap(ctx, tx, identity, connection)
		if err != nil {
			return err
		}
		var previous []byte
		err = tx.QueryRow(ctx, `SELECT fingerprint FROM trap_ingestions WHERE trap_id=$1 AND batch_id=$2`, trap.ID, batchID).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read reserved batch: %w", err)
		}
		if len(previous) > 0 && !bytes.Equal(previous, hash[:]) {
			return contract.NewError("batch_conflict")
		}
		snapshots := map[int64]profiles.Snapshot{}
		for _, event := range events {
			snapshot, ok := snapshots[event.ProfileRevision]
			if !ok {
				snapshot, err = issuedSnapshot(ctx, tx, trap.ID, event.ProfileRevision)
				if err != nil {
					return err
				}
				snapshots[event.ProfileRevision] = snapshot
			}
			if err := r.checkPayload(ctx, tx, trap.ID, event, snapshot, false); err != nil {
				return err
			}
			if err := checkSnapshot(event, snapshot); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO trap_ingestions(trap_id,batch_id,fingerprint) VALUES($1,$2,$3) ON CONFLICT(trap_id,batch_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint`, trap.ID, batchID, hash[:])
		if err != nil {
			return fmt.Errorf("pin event batch: %w", err)
		}
		return nil
	})
}
