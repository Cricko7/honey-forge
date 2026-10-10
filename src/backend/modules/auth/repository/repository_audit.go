package repository

import (
	"context"
	"fmt"
	authcore "honey-forge/modules/auth"

	"github.com/jackc/pgx/v5"
)

// Audit and its durable notification commit with the business mutation.
func appendAudit(ctx context.Context, tx pgx.Tx, userID, action, kind, resourceID string) error {
	_, err := tx.Exec(ctx, `
		WITH entry AS (
			INSERT INTO auth_audit (id, organization_id, actor_user_id, actor_email, actor_role, action, resource_kind, resource_id)
			SELECT $1, organization_id, id, email, role, $3, $4, $5
			FROM users WHERE id = $2
			RETURNING *
		)
		INSERT INTO auth_changes (organization_id, type, data)
		SELECT organization_id, 'audit.created', jsonb_build_object(
			'id', id, 'occurred_at', occurred_at,
			'actor', jsonb_build_object('user_id', actor_user_id, 'email', actor_email, 'role', actor_role),
			'action', action, 'resource', jsonb_build_object('kind', resource_kind, 'id', resource_id),
			'details', details
		) FROM entry
	`, authcore.NewUUID(), userID, action, kind, resourceID)
	if err != nil {
		return fmt.Errorf("recording auth audit and notification: %w", err)
	}

	return nil
}
