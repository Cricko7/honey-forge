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
	// Publication is durable and ordered with each organization's other changes.
	// A restart with an identical catalog produces no duplicate invalidation.
	var previous string
	err = tx.QueryRow(ctx, `SELECT etag FROM frontend_catalog_state WHERE singleton`).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read catalog stream state: %w", err)
	}
	if previous != service.etag {
		if _, err := tx.Exec(ctx, `INSERT INTO frontend_catalog_state(singleton,etag) VALUES(true,$1) ON CONFLICT(singleton) DO UPDATE SET etag=EXCLUDED.etag`, service.etag); err != nil {
			return fmt.Errorf("update catalog stream state: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT id::text FROM organizations ORDER BY id`)
		if err != nil {
			return fmt.Errorf("list catalog recipients: %w", err)
		}
		var organizations []string
		for rows.Next() {
			var org string
			if err := rows.Scan(&org); err != nil {
				rows.Close()
				return fmt.Errorf("scan catalog recipient: %w", err)
			}
			organizations = append(organizations, org)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate catalog recipients: %w", err)
		}
		for _, org := range organizations {
			if _, err := tx.Exec(ctx, `INSERT INTO organization_changes(organization_id) VALUES($1) ON CONFLICT DO NOTHING`, org); err != nil {
				return fmt.Errorf("initialize catalog journal: %w", err)
			}
			var seq int64
			if err := tx.QueryRow(ctx, `UPDATE organization_changes SET sequence=sequence+1 WHERE organization_id=$1 RETURNING sequence`, org).Scan(&seq); err != nil {
				return fmt.Errorf("advance catalog journal: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO mutation_changes(organization_id,sequence,type,resource_id,metadata,data,created_at) VALUES($1,$2,'catalog.changed',$1,'{}',jsonb_build_object('etag',$3::text),clock_timestamp())`, org, seq, service.etag); err != nil {
				return fmt.Errorf("publish catalog change: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit catalog installation: %w", err)
	}
	return nil
}
