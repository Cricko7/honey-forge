package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository guards installation across restarts. PostgreSQL retains published
// versions; operator APIs cannot create, update or delete them.
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Install(ctx context.Context, service *Service) (result error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin catalog installation: %w", err)
	}
	defer func() {
		// Rollback after commit returns ErrTxClosed, so only roll back a failed install.
		if result != nil {
			if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				result = fmt.Errorf("catalog installation failed (%w); rollback: %w", result, err)
			}
		}
	}()
	// Serialize installations from replicas before checking the complete retained set.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(71893654003)`); err != nil {
		return fmt.Errorf("lock catalog installation: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT type_id,type_version FROM catalog_versions`)
	if err != nil {
		return fmt.Errorf("read retained catalog: %w", err)
	}
	for rows.Next() {
		var key typeKey
		if err := rows.Scan(&key.id, &key.version); err != nil {
			rows.Close()
			return fmt.Errorf("read catalog version: %w", err)
		}
		if service.byType[key] == nil {
			rows.Close()
			return fmt.Errorf("installed catalog must retain published version %s/%d", key.id, key.version)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read catalog versions: %w", err)
	}
	for _, entry := range service.entries {
		id, version := string(entry.entry.TypeID), entry.entry.TypeVersion
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_versions(type_id,type_version,entry) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, version, entry.raw); err != nil {
			return fmt.Errorf("install catalog version: %w", err)
		}
		var same bool
		if err := tx.QueryRow(ctx, `SELECT entry=$3::jsonb FROM catalog_versions WHERE type_id=$1 AND type_version=$2`, id, version, entry.raw).Scan(&same); err != nil {
			return fmt.Errorf("compare catalog version: %w", err)
		}
		if !same {
			return fmt.Errorf("published catalog version %s/%d is immutable; publish a new version", id, version)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit catalog installation: %w", err)
	}
	return nil
}
