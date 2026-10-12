package traplinks

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool}
}

const liveLinks = `FROM trap_links l JOIN traps s ON s.id=l.source_id AND s.organization_id=l.organization_id JOIN traps t ON t.id=l.target_id AND t.organization_id=l.organization_id WHERE l.organization_id=$1 AND s.deleted_at IS NULL AND t.deleted_at IS NULL`

func (r *Repository) List(ctx context.Context, org string) ([]Link, error) {
	rows, err := r.pool.Query(ctx, `SELECT l.source_id::text,l.target_id::text `+liveLinks+` ORDER BY l.source_id,l.target_id`, org)
	if err != nil {
		return nil, fmt.Errorf("list trap links: %w", err)
	}

	defer rows.Close()

	links, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Link])
	if err != nil {
		return nil, fmt.Errorf("read trap links: %w", err)
	}

	if links == nil {
		links = []Link{}
	}

	return links, nil
}

func (r *Repository) Create(ctx context.Context, org string, link Link) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Lock in ID order to serialize with tombstones without opposite-link deadlocks.
		rows, err := tx.Query(ctx, `SELECT id::text FROM traps WHERE organization_id=$1 AND id IN ($2::uuid,$3::uuid) AND deleted_at IS NULL ORDER BY id FOR SHARE`, org, link.SourceID, link.TargetID)
		if err != nil {
			return fmt.Errorf("lock link endpoints: %w", err)
		}

		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("read link endpoints: %w", err)
		}

		if len(ids) != 2 {
			return contract.NewError("resource_not_found")
		}

		result, err := tx.Exec(ctx, `INSERT INTO trap_links(organization_id,source_id,target_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, org, link.SourceID, link.TargetID)
		if err != nil {
			return fmt.Errorf("create trap link: %w", err)
		}

		if result.RowsAffected() == 0 {
			return contract.NewError("trap_link_exists")
		}

		return nil
	})
}

func (r *Repository) Delete(ctx context.Context, org, source, target string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM trap_links WHERE organization_id=$1 AND source_id=$2 AND target_id=$3`, org, source, target)
	if err != nil {
		return fmt.Errorf("delete trap link: %w", err)
	}

	if result.RowsAffected() == 0 {
		return contract.NewError("resource_not_found")
	}

	return nil
}

func (r *Repository) Targets(ctx context.Context, org, source string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT l.target_id::text `+liveLinks+` AND l.source_id=$2 ORDER BY l.target_id`, org, source)
	if err != nil {
		return nil, fmt.Errorf("find traffic targets: %w", err)
	}

	defer rows.Close()

	targets, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read traffic targets: %w", err)
	}

	return targets, nil
}
