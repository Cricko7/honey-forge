package traps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/modules/agentws"
	"honey-forge/modules/profiles"
)

func (r *Repository) EnqueuePipeline(ctx context.Context, identity agentws.Identity, request pipeline.Request) error {
	if err := requirePipelineIdentity(ctx, identity); err != nil {
		return err
	}

	if err := pipeline.Check(request); err != nil {
		return err
	}

	raw, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode pipeline request: %w", err)
	}

	hash, err := mutation.Fingerprint(raw)
	if err != nil {
		return err
	}

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Serialize the organization quota and duplicate admission across API instances.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 713))`, identity.OrganizationID); err != nil {
			return fmt.Errorf("lock pipeline admission: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM http_pipeline_traces WHERE organization_id=$1 AND expires_at<=now()`, identity.OrganizationID); err != nil {
			return fmt.Errorf("expire pipeline traces: %w", err)
		}

		var previous []byte
		err := tx.QueryRow(ctx, `SELECT fingerprint FROM http_pipeline_deliveries WHERE source_id=$1 AND request_id=$2`, identity.TrapID, request.RequestID).Scan(&previous)
		if err == nil {
			if !bytes.Equal(previous, hash[:]) {
				return fmt.Errorf("pipeline request ID conflict")
			}

			return nil
		}

		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read pipeline admission: %w", err)
		}

		var targetGeneration int64
		var configuration []byte
		err = tx.QueryRow(ctx, `SELECT t.generation,t.applied_configuration FROM traps s JOIN trap_links l ON l.source_id=s.id AND l.organization_id=s.organization_id JOIN traps t ON t.id=l.target_id AND t.organization_id=l.organization_id WHERE s.id=$1 AND s.organization_id=$2 AND s.generation=$3 AND t.id=$4 AND s.deleted_at IS NULL AND t.deleted_at IS NULL AND t.type_id='custom-http' AND t.applied_configuration IS NOT NULL`, identity.TrapID, identity.OrganizationID, identity.CredentialGeneration, request.TargetID).Scan(&targetGeneration, &configuration)
		if err != nil {
			return fmt.Errorf("read authorized pipeline destination: %w", err)
		}

		var snapshot profiles.Snapshot
		if err := json.Unmarshal(configuration, &snapshot); err != nil {
			return fmt.Errorf("decode pipeline destination: %w", err)
		}

		if !pipeline.AllowsReceive(snapshot, request) {
			return fmt.Errorf("pipeline endpoint unavailable")
		}

		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM http_pipeline_deliveries WHERE organization_id=$1`, identity.OrganizationID).Scan(&count); err != nil {
			return fmt.Errorf("read pipeline capacity: %w", err)
		}

		if count >= pipeline.MaxHops {
			return fmt.Errorf("pipeline capacity reached")
		}

		traceID := string(contract.NewID())
		expires := time.Now().UTC().Add(pipeline.Lifetime)
		ip, port := request.SourceIP, request.SourcePort
		if request.Token == "" {
			if _, err := tx.Exec(ctx, `INSERT INTO http_pipeline_traces(id,organization_id,source_ip,source_port,expires_at) VALUES($1,$2,$3,$4,$5)`, traceID, identity.OrganizationID, ip, port, expires); err != nil {
				return fmt.Errorf("create pipeline trace: %w", err)
			}
		} else {
			// Continuations survive reconnects, but remain bound to the receiver's credential generation.
			err := tx.QueryRow(ctx, `SELECT tr.id::text,host(tr.source_ip),tr.source_port,tr.expires_at FROM http_pipeline_deliveries d JOIN http_pipeline_traces tr ON tr.id=d.trace_id WHERE d.id=$1 AND d.target_id=$2 AND d.organization_id=$3 AND d.target_generation=$4 AND tr.expires_at>now()`, request.Token, identity.TrapID, identity.OrganizationID, identity.CredentialGeneration).Scan(&traceID, &ip, &port, &expires)
			if err != nil {
				return fmt.Errorf("read pipeline continuation: %w", err)
			}
		}

		var hop int
		if err := tx.QueryRow(ctx, `UPDATE http_pipeline_traces SET hops=hops+1 WHERE id=$1 AND hops<$2 AND expires_at>now() RETURNING hops`, traceID, pipeline.MaxHops).Scan(&hop); err != nil {
			return fmt.Errorf("advance pipeline trace: %w", err)
		}

		delivery := pipeline.Delivery{Request: request, SourceID: identity.TrapID, TraceID: traceID, Hop: hop, ExpiresAt: expires}
		delivery.Token = string(contract.NewID())
		delivery.SourceIP, delivery.SourcePort = ip, port
		encoded, err := json.Marshal(delivery)
		if err != nil {
			return fmt.Errorf("encode pipeline delivery: %w", err)
		}

		if _, err := tx.Exec(ctx, `INSERT INTO http_pipeline_deliveries(id,trace_id,organization_id,source_id,target_id,source_generation,target_generation,request_id,fingerprint,delivery) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, delivery.Token, traceID, identity.OrganizationID, identity.TrapID, request.TargetID, identity.CredentialGeneration, targetGeneration, request.RequestID, hash[:], encoded); err != nil {
			return fmt.Errorf("persist pipeline delivery: %w", err)
		}

		return nil
	})
}

func requirePipelineIdentity(ctx context.Context, identity agentws.Identity) error {
	if err := contract.RequireAgentTrap(ctx, contract.ID(identity.TrapID)); err != nil {
		return err
	}

	return contract.RequireOrganization(ctx, contract.ID(identity.OrganizationID))
}
