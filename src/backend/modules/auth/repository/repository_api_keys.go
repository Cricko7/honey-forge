package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	authcore "honey-forge/modules/auth"
)

func (r *Repository) CreateAPIKey(ctx context.Context, key authcore.APIKey) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO operator_api_keys (id, user_id, name, prefix, token_hash, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`, key.ID, key.UserID, key.Name, key.Prefix, key.Hash, key.CreatedAt)
		if err != nil {
			return fmt.Errorf("inserting API key: %w", err)
		}

		return appendAudit(ctx, tx, key.UserID, "api_key.created", "api_key", key.ID)
	})

	return databaseError("creating API key", err)
}

func (r *Repository) ListAPIKeys(ctx context.Context, organizationID string) ([]authcore.APIKey, error) {
	rows, err := r.pool.Query(ctx, `SELECT k.id::text, k.name, k.prefix, k.created_at
		FROM operator_api_keys k JOIN users u ON u.id = k.user_id
		WHERE u.organization_id = $1 ORDER BY k.created_at DESC, k.id`, organizationID)
	if err != nil {
		return nil, databaseError("listing API keys", err)
	}

	defer rows.Close()
	keys := []authcore.APIKey{}
	for rows.Next() {
		var key authcore.APIKey
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &key.CreatedAt); err != nil {
			return nil, databaseError("reading API key", err)
		}

		keys = append(keys, key)
	}

	return keys, databaseError("listing API keys", rows.Err())
}

func (r *Repository) RevokeAPIKey(ctx context.Context, actor authcore.AuthContext, id string) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var deleted string
		err := tx.QueryRow(ctx, `DELETE FROM operator_api_keys k USING users u
			WHERE k.user_id = u.id AND u.organization_id = $1 AND k.id = $2
			RETURNING k.id::text`, actor.OrganizationID, id).Scan(&deleted)
		if err != nil {
			return err
		}

		return appendAudit(ctx, tx, actor.UserID, "api_key.revoked", "api_key", deleted)
	})

	return databaseError("revoking API key", err)
}

func (r *Repository) ResolveAPIKey(ctx context.Context, hash []byte) (authcore.ResolvedSession, error) {
	var sess authcore.ResolvedSession
	err := r.pool.QueryRow(ctx, `SELECT k.id::text,
		u.id::text, u.organization_id::text, u.email, u.role, u.created_at,
		o.id::text, o.name, o.created_at
		FROM operator_api_keys k JOIN users u ON u.id = k.user_id
		JOIN organizations o ON o.id = u.organization_id WHERE k.token_hash = $1`, hash).
		Scan(&sess.ID, &sess.View.User.ID, &sess.View.User.OrganizationID,
			&sess.View.User.Email, &sess.View.User.Role, &sess.View.User.CreatedAt,
			&sess.View.Organization.ID, &sess.View.Organization.Name, &sess.View.Organization.CreatedAt)

	return sess, databaseError("resolving API key", err)
}

// The row lock makes the per-key minute quota atomic across API replicas.
func (r *Repository) ConsumeAPIKey(ctx context.Context, id string, limit int) error {
	return databaseError("consuming API key quota", pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var count int64
		var retry int
		err := tx.QueryRow(ctx, `SELECT rate_count,
			GREATEST(0, CEIL(EXTRACT(EPOCH FROM rate_window + interval '1 minute' - clock_timestamp())))::int
			FROM operator_api_keys WHERE id = $1 FOR UPDATE`, id).Scan(&count, &retry)
		if err != nil {
			return err
		}

		if retry > 0 && count >= int64(limit) {
			return &authcore.APIKeyRateError{RetryAfter: retry}
		}

		if retry == 0 {
			_, err = tx.Exec(ctx, `UPDATE operator_api_keys SET rate_window = clock_timestamp(), rate_count = 1 WHERE id = $1`, id)
		} else {
			_, err = tx.Exec(ctx, `UPDATE operator_api_keys SET rate_count = rate_count + 1 WHERE id = $1`, id)
		}

		return err
	}))
}
