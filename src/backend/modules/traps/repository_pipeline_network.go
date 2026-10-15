package traps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/mutation"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/modules/agentws"
	"honey-forge/modules/profiles"
)

func (r *Repository) enqueueNetworkPipeline(ctx context.Context, tx pgx.Tx, identity agentws.Identity, request pipeline.Request) error {
	var installed []byte
	err := tx.QueryRow(ctx, `SELECT applied_configuration FROM traps WHERE id=$1 AND organization_id=$2 AND generation=$3 AND deleted_at IS NULL AND runtime_state='running' AND applied_configuration IS NOT NULL`, identity.TrapID, identity.OrganizationID, identity.CredentialGeneration).Scan(&installed)
	if err != nil {
		return fmt.Errorf("read network pipeline source: %w", err)
	}

	var snapshot profiles.Snapshot
	if err := json.Unmarshal(installed, &snapshot); err != nil {
		return fmt.Errorf("decode network pipeline source: %w", err)
	}

	if !pipeline.AllowsSend(snapshot, request) {
		return fmt.Errorf("unconfigured network pipeline send")
	}

	raw, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode network pipeline request: %w", err)
	}

	hash, err := mutation.Fingerprint(raw)
	if err != nil {
		return fmt.Errorf("fingerprint network pipeline request: %w", err)
	}

	var previous []byte
	err = tx.QueryRow(ctx, `SELECT fingerprint FROM http_pipeline_routes WHERE source_id=$1 AND request_id=$2 AND source_generation=$3`, identity.TrapID, request.RequestID, identity.CredentialGeneration).Scan(&previous)
	if err == nil {
		if !bytes.Equal(previous, hash[:]) {
			return fmt.Errorf("network pipeline request ID conflict")
		}

		return nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read network pipeline admission: %w", err)
	}

	expires := time.Now().UTC().Add(pipeline.Lifetime)
	if request.Token != "" {
		err := tx.QueryRow(ctx, `SELECT tr.expires_at FROM http_pipeline_deliveries d JOIN http_pipeline_traces tr ON tr.id=d.trace_id WHERE d.id=$1 AND d.target_id=$2 AND d.organization_id=$3 AND d.target_generation=$4 AND tr.expires_at>now()`, request.Token, identity.TrapID, identity.OrganizationID, identity.CredentialGeneration).Scan(&expires)
		if err != nil {
			return fmt.Errorf("read network pipeline continuation: %w", err)
		}
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM http_pipeline_routes WHERE organization_id=$1`, identity.OrganizationID).Scan(&count); err != nil {
		return fmt.Errorf("read network pipeline capacity: %w", err)
	}

	if count >= pipeline.MaxHops {
		return fmt.Errorf("network pipeline capacity reached")
	}

	rows, err := tx.Query(ctx, `SELECT t.id::text FROM trap_links l JOIN traps t ON t.id=l.target_id AND t.organization_id=l.organization_id WHERE l.organization_id=$1 AND l.source_id=$2 AND t.deleted_at IS NULL AND t.type_id='custom-http' ORDER BY t.id`, identity.OrganizationID, identity.TrapID)
	if err != nil {
		return fmt.Errorf("read network pipeline destinations: %w", err)
	}

	targets, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("scan network pipeline destinations: %w", err)
	}

	for _, target := range targets {
		delivery := request
		delivery.TargetID = target
		delivery.RequestID = networkDeliveryID(request.RequestID, target)
		if err := r.enqueuePipelineDelivery(ctx, tx, identity, delivery); err != nil {
			return fmt.Errorf("enqueue network pipeline destination: %w", err)
		}
	}

	// Persist even an empty route so retries cannot deliver old traffic to a newly drawn arrow.
	_, err = tx.Exec(ctx, `INSERT INTO http_pipeline_routes(source_id,request_id,organization_id,source_generation,fingerprint,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, identity.TrapID, request.RequestID, identity.OrganizationID, identity.CredentialGeneration, hash[:], expires)
	if err != nil {
		return fmt.Errorf("persist network pipeline admission: %w", err)
	}

	return nil
}

func networkDeliveryID(request, target string) string {
	sum := sha256.Sum256([]byte(request + "\x00" + target))
	sum[6] = (sum[6] & 15) | 128
	sum[8] = (sum[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
