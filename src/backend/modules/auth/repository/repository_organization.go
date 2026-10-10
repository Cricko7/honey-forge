package repository

import (
	"context"
	"fmt"
	authcore "honey-forge/modules/auth"
	"math"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) Organization(ctx context.Context, id string) (authcore.Organization, error) {
	var org authcore.Organization

	err := r.pool.QueryRow(ctx, `
		SELECT id::text, name, created_at FROM organizations WHERE id = $1
	`, id).Scan(&org.ID, &org.Name, &org.CreatedAt)

	return org, databaseError("loading organization", err)
}

func (r *Repository) JoinCode(ctx context.Context, id string) (authcore.JoinCode, error) {
	var code authcore.JoinCode

	err := r.pool.QueryRow(ctx, `
		SELECT join_code, join_code_revision, join_code_rotated_at
		FROM organizations WHERE id = $1
	`, id).Scan(&code.Code, &code.Revision, &code.RotatedAt)

	return code, databaseError("loading join code", err)
}

func (r *Repository) RotateJoinCode(ctx context.Context, actor authcore.AuthContext, expected int32, next authcore.JoinCode) (authcore.JoinCode, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var revision int32

		err := tx.QueryRow(ctx, `
			SELECT join_code_revision FROM organizations WHERE id = $1 FOR UPDATE
		`, actor.OrganizationID).Scan(&revision)
		if err != nil {
			return fmt.Errorf("locking join code: %w", err)
		}
		if revision != expected {
			return authcore.ErrJoinCodeChanged
		}
		if revision == math.MaxInt32 {
			return authcore.ErrRevisionExhausted
		}

		next.Revision = revision + 1
		_, err = tx.Exec(ctx, `
			UPDATE organizations SET join_code = $1, join_code_revision = $2, join_code_rotated_at = $3
			WHERE id = $4
		`, next.Code, next.Revision, next.RotatedAt, actor.OrganizationID)
		if err != nil {
			return fmt.Errorf("updating join code: %w", err)
		}

		return appendAudit(ctx, tx, actor.UserID, "organization.join_code_rotated", "organization", actor.OrganizationID)
	})

	return next, databaseError("rotating organization join code", err)
}
