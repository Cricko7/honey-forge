package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	profilecore "honey-forge/src/backend/modules/profiles"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"honey-forge/src/backend/internal/platform/httpx"
)

type Repository struct {
	pool    *pgxpool.Pool
	writers Writers
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, writers: Writers{AppendAudit: appendAudit, AppendChange: appendChange}}
}

func (r *Repository) WithWriters(w Writers) *Repository { return &Repository{pool: r.pool, writers: w} }

const profileColumns = `p.id::text,p.organization_id::text,p.sequence,p.type_id,p.type_version,p.interaction_level,
	r.revision,r.name,r.description,r.config::text,r.secret_fields_set,r.updated_at,p.created_at`

func scanProfile(row pgx.Row) (profilecore.Profile, error) {
	var p profilecore.Profile
	var config string
	var secrets []byte
	err := row.Scan(&p.ID, &p.OrganizationID, &p.Sequence, &p.TypeID, &p.TypeVersion, &p.InteractionLevel, &p.Revision, &p.Name, &p.Description, &config, &secrets, &p.UpdatedAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, profilecore.ErrNotFound
	}

	if err != nil {
		return p, err
	}

	p.Config, err = httpx.DecodeObject([]byte(config))
	if err != nil {
		return p, fmt.Errorf("decoding stored config: %w", err)
	}

	if err := json.Unmarshal(secrets, &p.SecretFieldsSet); err != nil {
		return p, fmt.Errorf("decoding stored secret paths: %w", err)
	}

	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	return p, nil
}

func (r *Repository) ReadProfile(ctx context.Context, org, id string) (profilecore.Profile, error) {
	p, err := scanProfile(r.pool.QueryRow(ctx, `SELECT `+profileColumns+` FROM profiles p JOIN profile_revisions r ON r.profile_id=p.id AND r.revision=p.revision WHERE p.organization_id=$1 AND p.id=$2 AND p.deleted_at IS NULL`, org, id))
	return p, databaseError("reading profile", err)
}

func lockedProfile(ctx context.Context, tx pgx.Tx, org, id string) (profilecore.Profile, error) {
	var revision int32
	err := tx.QueryRow(ctx, `SELECT revision FROM profiles WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, org, id).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return profilecore.Profile{}, profilecore.ErrNotFound
	}

	if err != nil {
		return profilecore.Profile{}, err
	}

	// Read after acquiring the lock: a joined row can disappear during EPQ after a concurrent PATCH.
	return readRevision(ctx, tx, org, id, revision)
}

func databaseError(op string, err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	var connection *pgconn.ConnectError
	var network net.Error
	unavailable := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &connection) || errors.As(err, &network) || pgconn.SafeToRetry(err)
	if errors.As(err, &pgErr) {
		unavailable = unavailable || strings.HasPrefix(pgErr.Code, "08") || pgErr.Code == "53300" || pgErr.Code == "57014" || strings.HasPrefix(pgErr.Code, "57P0")
	}

	if unavailable {
		return fmt.Errorf("%s: %w", op, errors.Join(profilecore.ErrDatabaseUnavailable, err))
	}

	return fmt.Errorf("%s: %w", op, err)
}
