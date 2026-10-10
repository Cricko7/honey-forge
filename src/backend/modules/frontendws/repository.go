package frontendws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
)

type Change struct {
	Sequence   int64
	Type       string
	OccurredAt time.Time
	Data       json.RawMessage
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool} }

func (r *Repository) Boundary(ctx context.Context) (int64, int64, error) {
	if err := contract.AuthorizeCapability(ctx, contract.FrontendStream); err != nil {
		return 0, 0, err
	}
	p, _ := contract.PrincipalFrom(ctx)
	var boundary, floor int64
	err := r.pool.QueryRow(ctx, `SELECT COALESCE((SELECT sequence FROM organization_changes WHERE organization_id=$1),0),COALESCE((SELECT stream_floor FROM organization_changes WHERE organization_id=$1),0)`, string(p.OrganizationID)).Scan(&boundary, &floor)
	if err != nil {
		return 0, 0, fmt.Errorf("read stream boundary: %w", err)
	}
	return boundary, floor, nil
}

func (r *Repository) Changes(ctx context.Context, after, boundary int64) ([]Change, error) {
	if err := contract.AuthorizeCapability(ctx, contract.FrontendStream); err != nil {
		return nil, err
	}
	p, _ := contract.PrincipalFrom(ctx)
	rows, err := r.pool.Query(ctx, `SELECT sequence,type,created_at,data FROM mutation_changes WHERE organization_id=$1 AND sequence>$2 AND sequence<=$3 ORDER BY sequence LIMIT 100`, string(p.OrganizationID), after, boundary)
	if err != nil {
		return nil, fmt.Errorf("read frontend changes: %w", err)
	}
	defer rows.Close()
	changes := []Change{}
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.Sequence, &c.Type, &c.OccurredAt, &c.Data); err != nil {
			return nil, fmt.Errorf("scan frontend change: %w", err)
		}
		if len(c.Data) == 0 {
			return nil, fmt.Errorf("stream change lacks a durable DTO")
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate frontend changes: %w", err)
	}
	return changes, nil
}
