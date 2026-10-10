package traps

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
)

func agentIdentityAccess(ctx context.Context, identity agentws.Identity) error {
	if err := contract.RequireAgentTrap(ctx, contract.ID(identity.TrapID)); err != nil {
		return err
	}
	return contract.RequireOrganization(ctx, contract.ID(identity.OrganizationID))
}
func (r *Repository) IngestionTrap(ctx context.Context, tx pgx.Tx, identity agentws.Identity, connection string) (Record, error) {
	if err := agentIdentityAccess(ctx, identity); err != nil {
		return Record{}, err
	}
	current, err := lockRecord(ctx, tx, identity.OrganizationID, identity.TrapID)
	if err != nil {
		return Record{}, err
	}
	if current.Generation != identity.CredentialGeneration || len(current.TokenHash) == 0 || current.ConnectionID == nil || *current.ConnectionID != connection {
		return Record{}, contract.NewError("agent_unauthenticated")
	}
	return current, nil
}

// BeginIngestion is a durable fence shared with module 07. Every asynchronous
// acceptance must reserve before leaving the trap transaction.
func (r *Repository) BeginIngestion(ctx context.Context, identity agentws.Identity, connection, batchID string) error {
	if err := agentIdentityAccess(ctx, identity); err != nil {
		return err
	}
	if !contract.ValidID(batchID) {
		return contract.NewError("validation_failed")
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		current, err := lockRecord(ctx, tx, identity.OrganizationID, identity.TrapID)
		if err != nil {
			return err
		}
		if current.Generation != identity.CredentialGeneration || len(current.TokenHash) == 0 || current.ConnectionID == nil || *current.ConnectionID != connection {
			return contract.NewError("agent_unauthenticated")
		}
		_, err = tx.Exec(ctx, `INSERT INTO trap_ingestions(trap_id,batch_id) VALUES($1,$2) ON CONFLICT(trap_id,batch_id) DO NOTHING`, identity.TrapID, batchID)
		if err != nil {
			return fmt.Errorf("reserve ingestion: %w", err)
		}
		return nil
	})
}

// FinishIngestion is for module 07's reconciler after definitive persistence or
// rejection, including a completion belonging to a previously revoked token.
func (r *Repository) FinishIngestion(ctx context.Context, identity agentws.Identity, batchID string) error {
	if err := agentIdentityAccess(ctx, identity); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockRecord(ctx, tx, identity.OrganizationID, identity.TrapID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE trap_ingestions SET finished=true WHERE trap_id=$1 AND batch_id=$2`, identity.TrapID, batchID)
		if err != nil {
			return fmt.Errorf("finish ingestion: %w", err)
		}
		return nil
	})
}
func (g *Gateway) Ingest(ctx context.Context, identity agentws.Identity, connection string, batch agentws.TelemetryBatch) (agentws.TelemetryAck, error) {
	if g.ingester == nil {
		return agentws.TelemetryAck{}, contract.NewError("telemetry_unavailable")
	}
	if err := g.repository.BeginIngestion(ctx, identity, connection, batch.BatchID); err != nil {
		return agentws.TelemetryAck{}, err
	}
	ack, err := g.ingester.Ingest(ctx, identity, connection, batch)
	if err != nil {
		// Ambiguous errors retain the fence until the persistence reconciler settles
		// the batch. Only definitive validation/conflict errors can release it.
		var api *contract.Error
		if errors.As(err, &api) && (api.Status == 400 || api.Status == 422 || api.Code == "batch_conflict" || api.Code == "event_id_conflict") {
			if finishErr := g.repository.FinishIngestion(ctx, identity, batch.BatchID); finishErr != nil {
				return ack, errors.Join(err, finishErr)
			}
		}
		return ack, err
	}
	if ack.BatchID != batch.BatchID || ack.StoredAt.IsZero() || len(ack.AcknowledgedEventIDs) != len(batch.Events) {
		return ack, contract.NewError("ingestion_pending")
	}
	for i, event := range batch.Events {
		if ack.AcknowledgedEventIDs[i] != event.EventID {
			return ack, contract.NewError("ingestion_pending")
		}
	}
	if err := g.repository.FinishIngestion(ctx, identity, batch.BatchID); err != nil {
		return ack, err
	}
	return ack, nil
}
