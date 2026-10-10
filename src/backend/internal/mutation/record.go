package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"honey-forge/internal/contract"

	"github.com/jackc/pgx/v5"
)

func record(ctx context.Context, tx pgx.Tx, organizationID contract.ID, outcome Outcome) (int64, error) {
	principal, _ := contract.PrincipalFrom(ctx)
	metadata, err := json.Marshal(outcome.Metadata)
	if err != nil {
		return 0, fmt.Errorf("encode audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO mutation_audit(id,organization_id,actor_id,action,resource_id,metadata) VALUES($1,$2,$3,$4,$5,$6)`, string(contract.NewID()), string(organizationID), string(principal.UserID), outcome.Action, string(outcome.ResourceID), metadata); err != nil {
		return 0, fmt.Errorf("record audit: %w", err)
	}
	return recordChanges(ctx, tx, organizationID, outcome.Changes)
}
func recordChanges(ctx context.Context, tx pgx.Tx, organizationID contract.ID, changes []Change) (int64, error) {
	var sequence int64
	for _, change := range changes {
		metadata, err := json.Marshal(change.Metadata)
		if err != nil {
			return 0, fmt.Errorf("encode notification metadata: %w", err)
		}
		if err := tx.QueryRow(ctx, `UPDATE organization_changes SET sequence=sequence+1 WHERE organization_id=$1 RETURNING sequence`, string(organizationID)).Scan(&sequence); err != nil {
			return 0, fmt.Errorf("advance journal: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mutation_changes(organization_id,sequence,type,resource_id,metadata) VALUES($1,$2,$3,$4,$5)`, string(organizationID), sequence, change.Type, string(change.ResourceID), metadata); err != nil {
			return 0, fmt.Errorf("record notification: %w", err)
		}
	}
	return sequence, nil
}
