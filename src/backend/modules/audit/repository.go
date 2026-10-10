package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"honey-forge/internal/contract"
	"time"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(p *pgxpool.Pool) *Repository { return &Repository{pool: p} }

const columns = `id::text,occurred_at,actor_id::text,actor_email,actor_role,action,resource_kind,resource_id::text,details`

func scan(row pgx.Row) (Entry, error) {
	var e Entry
	var raw []byte
	err := row.Scan(&e.ID, &e.OccurredAt, &e.Actor.UserID, &e.Actor.Email, &e.Actor.Role, &e.Action, &e.Resource.Kind, &e.Resource.ID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, contract.NewError("resource_not_found")
	}
	if err != nil {
		return e, fmt.Errorf("read audit: %w", err)
	}
	if err := json.Unmarshal(raw, &e.Details); err != nil {
		return e, fmt.Errorf("decode audit: %w", err)
	}
	e.OccurredAt = e.OccurredAt.UTC()
	return e, nil
}
func (r *Repository) Read(ctx context.Context, org, id string) (Entry, error) {
	if err := contract.RequireOrganization(ctx, contract.ID(org)); err != nil {
		return Entry{}, err
	}
	return scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM audit_entries WHERE organization_id=$1 AND id=$2`, org, id))
}
func (r *Repository) List(ctx context.Context, org string, q Query) (items []Entry, more bool, boundary int64, err error) {
	if err := contract.RequireOrganization(ctx, contract.ID(org)); err != nil {
		return nil, false, 0, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, false, 0, fmt.Errorf("begin audit snapshot: %w", err)
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if e := tx.Rollback(rollback); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback audit snapshot: %w", e))
		}
	}()
	boundary = q.Boundary
	if boundary < 0 {
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT sequence FROM organization_changes WHERE organization_id=$1),0)`, org).Scan(&boundary); err != nil {
			return nil, false, 0, fmt.Errorf("capture audit boundary: %w", err)
		}
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM audit_entries WHERE organization_id=$1
 AND stream_sequence <= $2 AND ($3::timestamptz IS NULL OR occurred_at >= $3) AND ($4::timestamptz IS NULL OR occurred_at < $4)
 AND ($5::text='' OR action=$5) AND ($6::uuid IS NULL OR actor_id=$6) AND ($7::uuid IS NULL OR resource_id=$7)
 AND ($8::timestamptz IS NULL OR (occurred_at,id)<($8,$9::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $10`, org, boundary, q.From, q.To, q.Action, nullableID(q.ActorID), nullableID(q.ResourceID), q.AfterTime, nullableID(q.AfterID), q.Limit+1)
	if err != nil {
		return nil, false, 0, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()
	items = []Entry{}
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, false, 0, err
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, 0, fmt.Errorf("iterate audit: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, false, 0, fmt.Errorf("commit audit snapshot: %w", err)
	}
	more = len(items) > q.Limit
	if more {
		items = items[:q.Limit]
	}
	return items, more, boundary, nil
}
func nullableID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
