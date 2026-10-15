package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"honey-forge/modules/auth"
)

func (r *Repository) RemoveMember(ctx context.Context, actor auth.AuthContext, id string) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var allowed, confirmed bool
		var creatorID *string
		err := tx.QueryRow(ctx, `SELECT o.creator_user_id::text,
			COALESCE(o.creator_user_id = actor.id, false) AND actor.role = 'admin',
			EXISTS (SELECT 1 FROM operator_sessions s WHERE s.id = $3 AND s.user_id = actor.id
			AND s.expires_at > clock_timestamp() AND s.admin_verified_until > clock_timestamp())
			FROM organizations o JOIN users actor ON actor.organization_id = o.id
			WHERE o.id = $1 AND actor.id = $2 AND actor.removed_at IS NULL
			FOR NO KEY UPDATE OF o, actor`, actor.OrganizationID, actor.UserID, actor.SessionID).
			Scan(&creatorID, &allowed, &confirmed)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
			return auth.ErrForbidden
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return auth.ErrAdminConfirmationRequired
		}
		var role string
		err = tx.QueryRow(ctx, `SELECT role FROM users
			WHERE organization_id = $1 AND id = $2 AND removed_at IS NULL FOR NO KEY UPDATE`, actor.OrganizationID, id).Scan(&role)
		if err != nil {
			return err
		}
		if id == actor.UserID || (creatorID != nil && id == *creatorID) || role == auth.RoleAdmin {
			return auth.ErrMemberProtected
		}
		// NO KEY UPDATE preserves audit FK key-share locks while excluding credential
		// issuance's SHARE locks. Organization then user is the member-write lock order.
		// The user lock also serializes session/key issuance, preventing a stale login
		// from recreating credentials after the credential deletion below commits.
		if _, err := tx.Exec(ctx, `UPDATE users SET removed_at = clock_timestamp(), trap_management = false
			WHERE organization_id = $1 AND id = $2`, actor.OrganizationID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM operator_sessions WHERE user_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM operator_api_keys WHERE user_id = $1`, id); err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor.UserID, "member.removed", "user", id)
	})
	return databaseError("removing member, credentials and audit", err)
}
