package repository

import (
	"context"
	"errors"
	"fmt"
	authcore "honey-forge/src/backend/modules/auth"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Register(ctx context.Context, user authcore.User, org authcore.Organization, input authcore.OrganizationInput, hash string, sess authcore.Session, code authcore.JoinCode) (authcore.Organization, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if input.Mode == "join" {
			// SHARE conflicts with rotation's UPDATE and holds through user/session commit.
			err := tx.QueryRow(ctx, `
				SELECT id::text, name, created_at
				FROM organizations WHERE join_code = $1 FOR SHARE
			`, input.JoinCode).Scan(&org.ID, &org.Name, &org.CreatedAt)
			if errors.Is(err, pgx.ErrNoRows) {
				return authcore.ErrInvalidJoinCode
			}
			if err != nil {
				return fmt.Errorf("locking join organization: %w", err)
			}

			user.OrganizationID = org.ID
		} else {
			_, err := tx.Exec(ctx, `
				INSERT INTO organizations (id, name, created_at, join_code, join_code_revision, join_code_rotated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, org.ID, org.Name, org.CreatedAt, code.Code, code.Revision, code.RotatedAt)
			if err != nil {
				return fmt.Errorf("inserting organization: %w", err)
			}
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO users (id, organization_id, email, password_hash, role, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, user.ID, user.OrganizationID, user.Email, hash, user.Role, user.CreatedAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
				return authcore.ErrEmailTaken
			}

			return fmt.Errorf("inserting operator: %w", err)
		}

		action := "organization.created"
		if input.Mode == "join" {
			action = "organization.viewer_joined"
		}
		if err := appendAudit(ctx, tx, user.ID, action, "organization", org.ID); err != nil {
			return err
		}

		return insertSession(ctx, tx, sess)
	})

	return org, databaseError("registering operator and session", err)
}

func (r *Repository) Credentials(ctx context.Context, email string) (authcore.User, authcore.Organization, string, error) {
	var user authcore.User
	var org authcore.Organization
	var hash string

	err := r.pool.QueryRow(ctx, `
		SELECT u.id::text, u.organization_id::text, u.email, u.role, u.created_at,
			o.id::text, o.name, o.created_at, u.password_hash
		FROM users u JOIN organizations o ON o.id = u.organization_id
		WHERE u.email = $1
	`, email).Scan(&user.ID, &user.OrganizationID, &user.Email, &user.Role, &user.CreatedAt,
		&org.ID, &org.Name, &org.CreatedAt, &hash)

	return user, org, hash, databaseError("loading credentials", err)
}

func databaseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return authcore.ErrNotFound
	}

	var pgErr *pgconn.PgError
	var connectErr *pgconn.ConnectError
	var netErr net.Error

	unavailable := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.As(err, &connectErr) || errors.As(err, &netErr) || pgconn.SafeToRetry(err)

	if errors.As(err, &pgErr) {
		unavailable = unavailable || strings.HasPrefix(pgErr.Code, "08") ||
			pgErr.Code == "53300" || pgErr.Code == "57014" ||
			pgErr.Code == "57P01" || pgErr.Code == "57P02" || pgErr.Code == "57P03"
	}
	if unavailable {
		return fmt.Errorf("%s: %w", operation, errors.Join(authcore.ErrUnavailable, err))
	}

	return fmt.Errorf("%s: %w", operation, err)
}
