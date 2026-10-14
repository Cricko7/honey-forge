package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"honey-forge/modules/agentws"
)

// Persist the workflow receipt together with its acknowledged telemetry event.
func completePipeline(ctx context.Context, tx pgx.Tx, identity agentws.Identity, event AgentEvent) error {
	if event.TypeID != "custom-http" || event.EventType != "http.response" && event.EventType != "http.failure" {
		return nil
	}

	var data struct {
		PipelineID string `json:"pipeline_id"`
		Step       int    `json:"pipeline_step"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return fmt.Errorf("decode pipeline completion: %w", err)
	}

	if data.PipelineID == "" {
		return nil
	}

	status := "succeeded"
	if event.EventType == "http.failure" {
		status = "failed"
	}

	_, err := tx.Exec(ctx, `UPDATE http_pipeline_deliveries SET status=$1,completed_at=now() WHERE trace_id=$2 AND (delivery->>'hop')::integer=$3 AND organization_id=$4 AND target_id=$5 AND target_generation=$6 AND status='pending' AND attempts>0`, status, data.PipelineID, data.Step, identity.OrganizationID, identity.TrapID, identity.CredentialGeneration)
	if err != nil {
		return fmt.Errorf("persist pipeline completion: %w", err)
	}

	return nil
}
