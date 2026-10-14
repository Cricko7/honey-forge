package traps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/profiles"
)

// PipelineSnapshot reads the installed configuration, fenced by the live credential and connection.
func (r *Repository) PipelineSnapshot(ctx context.Context, identity agentws.Identity, connection string) (profiles.Snapshot, error) {
	var snapshot profiles.Snapshot
	if err := contract.RequireAgentTrap(ctx, contract.ID(identity.TrapID)); err != nil {
		return snapshot, err
	}
	if err := contract.RequireOrganization(ctx, contract.ID(identity.OrganizationID)); err != nil {
		return snapshot, err
	}
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT applied_configuration FROM traps WHERE organization_id=$1 AND id=$2 AND generation=$3 AND connection_id=$4 AND deleted_at IS NULL AND type_id='custom-http' AND runtime_state='running' AND applied_configuration IS NOT NULL`, identity.OrganizationID, identity.TrapID, identity.CredentialGeneration, connection).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, contract.NewError("resource_not_found")
	}
	if err != nil {
		return snapshot, fmt.Errorf("read pipeline snapshot: %w", err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode pipeline snapshot: %w", err)
	}
	return snapshot, nil
}
