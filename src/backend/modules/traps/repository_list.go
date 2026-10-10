package traps

import (
	"context"
	"fmt"
	"time"

	"honey-forge/internal/contract"
)

func (r *Repository) List(ctx context.Context, org string, q ListQuery) ([]Trap, bool, error) {
	if err := readAccess(ctx, org); err != nil {
		return nil, false, err
	}
	if q.ProfileID != "" {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM profiles WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL)`, org, q.ProfileID).Scan(&exists); err != nil {
			return nil, false, fmt.Errorf("check profile filter: %w", err)
		}
		if !exists {
			return nil, false, contract.NewError("resource_not_found")
		}
	}
	rows, err := r.pool.Query(ctx, `SELECT `+columns+` FROM traps WHERE organization_id=$1 AND deleted_at IS NULL AND ($2::uuid IS NULL OR profile_id=$2) AND ($3::text='' OR connectivity=$3) AND (created_at,id)<=($4,$5::uuid) AND ($6::timestamptz IS NULL OR (created_at,id)<($6,$7::uuid)) ORDER BY created_at DESC,id DESC LIMIT $8`, org, nullID(q.ProfileID), q.Connectivity, q.Boundary, q.BoundaryID, nullTime(q.AfterTime), nullID(q.AfterID), q.Limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list traps: %w", err)
	}
	defer rows.Close()
	items := []Trap{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, r.Trap)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate traps: %w", err)
	}
	more := len(items) > q.Limit
	if more {
		items = items[:q.Limit]
	}
	return items, more, nil
}
func nullID(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
