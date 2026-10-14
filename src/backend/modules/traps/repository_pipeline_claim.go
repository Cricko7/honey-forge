package traps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/modules/agentws"
)

func (r *Repository) ClaimPipeline(ctx context.Context, identity agentws.Identity, connection string) (*pipeline.Delivery, error) {
	if err := requirePipelineIdentity(ctx, identity); err != nil {
		return nil, err
	}

	snapshot, err := r.PipelineSnapshot(ctx, identity, connection)
	if err != nil {
		// Stopped or unconfigured receivers retain their pending deliveries until expiry.
		return nil, err
	}

	var result *pipeline.Delivery
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var id string
		var raw []byte
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT d.id::text,d.delivery, EXISTS(SELECT 1 FROM trap_links l JOIN traps s ON s.id=l.source_id WHERE l.organization_id=d.organization_id AND l.source_id=d.source_id AND l.target_id=d.target_id AND s.generation=d.source_generation AND s.deleted_at IS NULL) AND d.target_generation=$3 FROM http_pipeline_deliveries d JOIN http_pipeline_traces tr ON tr.id=d.trace_id WHERE d.organization_id=$1 AND d.target_id=$2 AND d.status='pending' AND d.lease_until<=now() AND tr.expires_at>now() ORDER BY d.created_at,d.id FOR UPDATE OF d SKIP LOCKED LIMIT 1`, identity.OrganizationID, identity.TrapID, identity.CredentialGeneration).Scan(&id, &raw, &allowed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("claim pipeline delivery: %w", err)
		}

		var delivery pipeline.Delivery
		if err := json.Unmarshal(raw, &delivery); err != nil {
			return fmt.Errorf("decode queued pipeline delivery: %w", err)
		}

		if !allowed || !pipeline.AllowsReceive(snapshot, delivery.Request) {
			if _, err := tx.Exec(ctx, `UPDATE http_pipeline_deliveries SET status='cancelled',completed_at=now() WHERE id=$1`, id); err != nil {
				return fmt.Errorf("cancel revoked pipeline delivery: %w", err)
			}

			return nil
		}

		// Longer than the maximum local flow timeout; lost acknowledgements are retried.
		if _, err := tx.Exec(ctx, `UPDATE http_pipeline_deliveries SET lease_until=now()+interval '45 seconds',attempts=attempts+1 WHERE id=$1`, id); err != nil {
			return fmt.Errorf("lease pipeline delivery: %w", err)
		}

		result = &delivery

		return nil
	})

	return result, err
}
