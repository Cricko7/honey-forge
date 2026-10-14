package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"honey-forge/modules/auth"
)

func (r *Repository) AdminAccess(ctx context.Context, actor auth.AuthContext) (auth.AdminAccessView, error) {
	var view auth.AdminAccessView
	err := r.pool.QueryRow(ctx, `SELECT CASE WHEN s.admin_verified_until > now() THEN s.admin_verified_until END
		FROM operator_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id = $1 AND u.id = $2 AND u.organization_id = $3 AND u.role = 'admin' AND s.expires_at > now()`,
		actor.SessionID, actor.UserID, actor.OrganizationID).Scan(&view.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, auth.ErrUnauthorized
	}
	return view, databaseError("checking admin confirmation", err)
}

// An atomic per-user quota is shared by every browser session and API replica.
// All attempts count, including successful checks; new logins do not reset it.
func (r *Repository) ConsumeAdminAttempt(ctx context.Context, actor auth.AuthContext) error {
	result, err := r.pool.Exec(ctx, `UPDATE users u SET
		admin_auth_attempts = CASE WHEN admin_auth_window <= now() - interval '15 minutes' THEN 1 ELSE admin_auth_attempts + 1 END,
		admin_auth_window = CASE WHEN admin_auth_window <= now() - interval '15 minutes' THEN now() ELSE admin_auth_window END
		WHERE u.id = $1 AND u.organization_id = $2 AND u.role = 'admin'
		AND EXISTS (SELECT 1 FROM operator_sessions s WHERE s.id = $3 AND s.user_id = u.id AND s.expires_at > now())
		AND (admin_auth_window <= now() - interval '15 minutes' OR admin_auth_attempts < 5)`, actor.UserID, actor.OrganizationID, actor.SessionID)
	if err != nil {
		return databaseError("consuming admin password attempt", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	if _, err := r.AdminAccess(ctx, actor); err != nil {
		return err
	}
	var retry int
	err = r.pool.QueryRow(ctx, `SELECT GREATEST(1, CEIL(EXTRACT(EPOCH FROM admin_auth_window + interval '15 minutes' - now())))::int
		FROM users WHERE id = $1`, actor.UserID).Scan(&retry)
	if err != nil {
		return databaseError("reading admin password quota", err)
	}
	return &auth.AdminPasswordRateError{RetryAfter: retry}
}

func (r *Repository) ConfirmAdminAccess(ctx context.Context, actor auth.AuthContext, expectedHash string) (auth.AdminAccessView, error) {
	var view auth.AdminAccessView
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `UPDATE operator_sessions s SET admin_verified_until = LEAST(s.expires_at, now() + interval '15 minutes')
			FROM users u WHERE s.id = $1 AND s.user_id = u.id AND u.id = $2 AND u.organization_id = $3
			AND u.role = 'admin' AND u.password_hash = $4 AND s.expires_at > now() RETURNING s.admin_verified_until`,
			actor.SessionID, actor.UserID, actor.OrganizationID, expectedHash).Scan(&view.ExpiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrUnauthorized
		}
		if err != nil {
			return err
		}
		return appendAudit(ctx, tx, actor.UserID, "session.admin_confirmed", "session", actor.SessionID)
	})
	return view, databaseError("confirming admin session and audit", err)
}

func (r *Repository) LockAdminAccess(ctx context.Context, actor auth.AuthContext) error {
	if _, err := r.AdminAccess(ctx, actor); err != nil {
		return err
	}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `UPDATE operator_sessions s SET admin_verified_until = NULL FROM users u
			WHERE s.id = $1 AND s.user_id = u.id AND u.id = $2 AND u.organization_id = $3
			AND u.role = 'admin' AND s.expires_at > now() AND s.admin_verified_until IS NOT NULL`,
			actor.SessionID, actor.UserID, actor.OrganizationID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return nil
		}
		return appendAudit(ctx, tx, actor.UserID, "session.admin_locked", "session", actor.SessionID)
	})
	return databaseError("locking admin session and audit", err)
}
