package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
)

func record(ctx context.Context, tx pgx.Tx, organizationID contract.ID, outcome Outcome) (int64, error) {
	principal, _ := contract.PrincipalFrom(ctx)
	metadata, err := json.Marshal(outcome.Metadata)
	if err != nil {
		return 0, fmt.Errorf("encode audit metadata: %w", err)
	}
	details, err := json.Marshal(outcome.AuditDetails)
	if err != nil {
		return 0, fmt.Errorf("encode audit details: %w", err)
	}
	id := contract.NewID()
	if _, err := tx.Exec(ctx, `INSERT INTO mutation_audit(id,organization_id,actor_id,action,resource_id,metadata,actor_email,actor_role,resource_kind,details) VALUES($1,$2,$3,$4,$5,$6,COALESCE((SELECT email FROM users WHERE id=$3 AND organization_id=$2),''),$7,$8,$9)`, string(id), string(organizationID), string(principal.UserID), outcome.Action, string(outcome.ResourceID), metadata, string(principal.Role), strings.SplitN(outcome.Action, ".", 2)[0], details); err != nil {
		return 0, fmt.Errorf("record audit: %w", err)
	}
	changes := append(append([]Change(nil), outcome.Changes...), Change{Type: "audit.created", ResourceID: id})
	return recordChanges(ctx, tx, organizationID, changes)
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
