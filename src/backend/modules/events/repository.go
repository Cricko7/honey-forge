package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/modules/agentws"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

type Repository struct {
	pool  *pgxpool.Pool
	traps *traps.Repository
}

func NewRepository(pool *pgxpool.Pool, traps *traps.Repository) *Repository {
	return &Repository{pool, traps}
}
func (r *Repository) Ingest(ctx context.Context, identity agentws.Identity, connection, batchID string, events []AgentEvent) (agentws.TelemetryAck, error) {
	var ack agentws.TelemetryAck
	batchRaw, err := json.Marshal(struct {
		Events []AgentEvent `json:"events"`
	}{events})
	if err != nil {
		return ack, fmt.Errorf("encode events: %w", err)
	}
	batchHash, err := mutation.Fingerprint(batchRaw)
	if err != nil {
		return ack, err
	}
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		org := identity.OrganizationID
		if _, err := tx.Exec(ctx, `INSERT INTO organization_changes(organization_id) VALUES($1) ON CONFLICT DO NOTHING`, org); err != nil {
			return fmt.Errorf("initialize event journal: %w", err)
		}
		var sequence int64
		if err := tx.QueryRow(ctx, `SELECT sequence FROM organization_changes WHERE organization_id=$1 FOR UPDATE`, org).Scan(&sequence); err != nil {
			return fmt.Errorf("lock event journal: %w", err)
		}
		trap, err := r.traps.IngestionTrap(ctx, tx, identity, connection)
		if err != nil {
			return err
		}
		var previous []byte
		var stored time.Time
		err = tx.QueryRow(ctx, `SELECT fingerprint,stored_at FROM trap_event_batches WHERE trap_id=$1 AND batch_id=$2`, trap.ID, batchID).Scan(&previous, &stored)
		if err == nil {
			if !bytes.Equal(previous, batchHash[:]) {
				return contract.NewError("batch_conflict")
			}
			ack = acknowledge(batchID, events, stored)
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read event batch: %w", err)
		}
		snapshots := map[int64]profiles.Snapshot{}
		stored = time.Now().UTC().Truncate(time.Microsecond)
		for _, event := range events {
			snapshot, ok := snapshots[event.ProfileRevision]
			if !ok {
				var raw []byte
				err := tx.QueryRow(ctx, `SELECT configuration FROM commands WHERE trap_id=$1 AND action='apply_config' AND target_profile_revision=$2 AND started_at IS NOT NULL ORDER BY created_at DESC LIMIT 1`, trap.ID, event.ProfileRevision).Scan(&raw)
				if errors.Is(err, pgx.ErrNoRows) {
					return contract.NewError("telemetry_invalid")
				}
				if err != nil {
					return fmt.Errorf("read event snapshot: %w", err)
				}
				if err := json.Unmarshal(raw, &snapshot); err != nil {
					return fmt.Errorf("decode event snapshot: %w", err)
				}
				snapshots[event.ProfileRevision] = snapshot
			}
			if err := checkSnapshot(event, snapshot); err != nil {
				return err
			}
			raw, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("encode event: %w", err)
			}
			hash, err := mutation.Fingerprint(raw)
			if err != nil {
				return err
			}
			var old []byte
			var owner string
			err = tx.QueryRow(ctx, `SELECT trap_id::text,fingerprint FROM trap_events WHERE event_id=$1`, event.EventID).Scan(&owner, &old)
			if err == nil {
				if owner != trap.ID || !bytes.Equal(old, hash[:]) {
					return contract.NewError("event_id_conflict")
				}
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("read existing event: %w", err)
			}
			var reused bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trap_events WHERE trap_id=$1 AND session_id=$2 AND session_sequence=$3)`, trap.ID, event.SessionID, event.SessionSequence).Scan(&reused); err != nil {
				return fmt.Errorf("check event sequence: %w", err)
			}
			if reused {
				return contract.NewError("telemetry_invalid")
			}
			at, err := time.Parse(time.RFC3339Nano, event.OccurredAt)
			if err != nil {
				return err
			}
			inserted, err := tx.Exec(ctx, `INSERT INTO trap_events(event_id,trap_id,organization_id,session_id,session_sequence,occurred_at,received_at,fingerprint,envelope) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb) ON CONFLICT(event_id) DO NOTHING`, event.EventID, trap.ID, org, event.SessionID, event.SessionSequence, at, stored, hash[:], string(raw))
			if err != nil {
				return fmt.Errorf("store event: %w", err)
			}
			if inserted.RowsAffected() == 0 {
				if err := tx.QueryRow(ctx, `SELECT trap_id::text,fingerprint FROM trap_events WHERE event_id=$1`, event.EventID).Scan(&owner, &old); err != nil {
					return fmt.Errorf("read raced event: %w", err)
				}
				if owner != trap.ID || !bytes.Equal(old, hash[:]) {
					return contract.NewError("event_id_conflict")
				}
				continue
			}
			if err := tx.QueryRow(ctx, `UPDATE organization_changes SET sequence=sequence+1 WHERE organization_id=$1 RETURNING sequence`, org).Scan(&sequence); err != nil {
				return fmt.Errorf("advance event journal: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO mutation_changes(organization_id,sequence,type,resource_id,metadata) VALUES($1,$2,'event.created',$3,'{}')`, org, sequence, event.EventID); err != nil {
				return fmt.Errorf("publish event: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO trap_event_batches(trap_id,batch_id,fingerprint,stored_at) VALUES($1,$2,$3,$4)`, trap.ID, batchID, batchHash[:], stored); err != nil {
			return fmt.Errorf("retain batch: %w", err)
		}
		// Finish in the same transaction as event persistence, even if the caller
		// loses its context/connection immediately after commit.
		if _, err := tx.Exec(ctx, `UPDATE trap_ingestions SET finished=true WHERE trap_id=$1 AND batch_id=$2`, trap.ID, batchID); err != nil {
			return fmt.Errorf("finish persisted batch: %w", err)
		}
		ack = acknowledge(batchID, events, stored)
		return nil
	})
	return ack, err
}
func acknowledge(batchID string, events []AgentEvent, stored time.Time) agentws.TelemetryAck {
	ack := agentws.TelemetryAck{BatchID: batchID, StoredAt: stored.UTC()}
	for _, event := range events {
		ack.AcknowledgedEventIDs = append(ack.AcknowledgedEventIDs, event.EventID)
	}
	return ack
}
