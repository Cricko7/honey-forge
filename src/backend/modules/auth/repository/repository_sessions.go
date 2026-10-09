package repository

import (
	"context"
	"errors"
	"fmt"
	authcore "honey-forge/src/backend/modules/auth"

	"github.com/jackc/pgx/v5"
)

func insertSession(ctx context.Context, tx pgx.Tx, sess authcore.Session) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO operator_sessions (id, token_hash, csrf_hash, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, sess.ID, sess.Hash, sess.CSRFHash, sess.UserID, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("inserting operator session: %w", err)
	}

	return appendAudit(ctx, tx, sess.UserID, "session.created", "session", sess.ID)
}

func (r *Repository) CreateSession(ctx context.Context, sess authcore.Session, oldHash []byte) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if len(oldHash) > 0 {
			if err := revokeSession(ctx, tx, oldHash); err != nil {
				return err
			}
		}

		return insertSession(ctx, tx, sess)
	})

	return databaseError("replacing operator session", err)
}

func (r *Repository) ResolveSession(ctx context.Context, hash []byte) (authcore.ResolvedSession, error) {
	var sess authcore.ResolvedSession

	err := r.pool.QueryRow(ctx, `
		SELECT s.id::text, s.csrf_hash, s.expires_at,
			u.id::text, u.organization_id::text, u.email, u.role, u.created_at,
			o.id::text, o.name, o.created_at
		FROM operator_sessions s
		JOIN users u ON u.id = s.user_id
		JOIN organizations o ON o.id = u.organization_id
		WHERE s.token_hash = $1 AND s.expires_at > now()
	`, hash).Scan(&sess.ID, &sess.CSRFHash, &sess.View.ExpiresAt,
		&sess.View.User.ID, &sess.View.User.OrganizationID, &sess.View.User.Email, &sess.View.User.Role, &sess.View.User.CreatedAt,
		&sess.View.Organization.ID, &sess.View.Organization.Name, &sess.View.Organization.CreatedAt)

	return sess, databaseError("loading active operator session", err)
}

func revokeSession(ctx context.Context, tx pgx.Tx, hash []byte) error {
	var id, userID string

	err := tx.QueryRow(ctx, `
		DELETE FROM operator_sessions WHERE token_hash = $1
		RETURNING id::text, user_id::text
	`, hash).Scan(&id, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}

	return appendAudit(ctx, tx, userID, "session.revoked", "session", id)
}

func (r *Repository) RevokeSession(ctx context.Context, hash []byte) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return revokeSession(ctx, tx, hash)
	})

	return databaseError("revoking operator session", err)
}
