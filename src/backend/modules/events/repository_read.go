package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
)

func readAccess(ctx context.Context) (string, error) {
	if err := contract.Authorize(ctx, contract.Admin, contract.Viewer); err != nil {
		return "", err
	}
	p, _ := contract.PrincipalFrom(ctx)
	return string(p.OrganizationID), ctx.Err()
}
func readEvent(row pgx.Row) (Event, error) {
	var event Event
	var raw []byte
	err := row.Scan(&raw, &event.TrapID, &event.ReceivedAt, &event.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return event, contract.NewError("resource_not_found")
	}
	if err != nil {
		return event, fmt.Errorf("read event: %w", err)
	}
	data := event.Data
	if err := json.Unmarshal(raw, &event.AgentEvent); err != nil {
		return event, fmt.Errorf("decode event: %w", err)
	}
	event.Data = data
	event.ReceivedAt = event.ReceivedAt.UTC()
	event.SourceEnrichment = &Enrichment{}
	return event, nil
}
func (r *Repository) Read(ctx context.Context, id string) (Event, error) {
	org, err := readAccess(ctx)
	if err != nil {
		return Event{}, err
	}
	return readEvent(r.pool.QueryRow(ctx, `SELECT envelope,trap_id::text,received_at,event_data FROM trap_events WHERE organization_id=$1 AND event_id=$2`, org, id))
}
func (r *Repository) List(ctx context.Context, q Query) ([]EventSummary, bool, int64, int64, error) {
	org, err := readAccess(ctx)
	if err != nil {
		return nil, false, 0, 0, err
	}
	if q.TrapID != "" {
		visible, err := r.traps.Visible(ctx, org, q.TrapID)
		if err != nil {
			return nil, false, 0, 0, err
		}
		if !visible {
			return nil, false, 0, 0, contract.NewError("resource_not_found")
		}
	}
	items := []EventSummary{}
	boundary := q.Boundary
	stream, err := mutation.NewStore(r.pool).Snapshot(ctx, contract.ID(org), func(ctx context.Context, tx pgx.Tx) error {
		if boundary < 0 {
			if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0) FROM trap_events WHERE organization_id=$1`, org).Scan(&boundary); err != nil {
				return fmt.Errorf("capture event boundary: %w", err)
			}
		}
		rows, err := tx.Query(ctx, `SELECT envelope,trap_id::text,received_at,event_data FROM trap_events WHERE organization_id=$1 AND sequence<=$2 AND ($3::uuid IS NULL OR trap_id=$3) AND ($4::timestamptz IS NULL OR (occurred_at,occurred_at_submicro) >= ($4,$13::smallint)) AND ($5::timestamptz IS NULL OR (occurred_at,occurred_at_submicro) < ($5,$14::smallint)) AND ($6::text='' OR envelope->>'event_type'=$6) AND ($7::uuid IS NULL OR session_id=$7) AND ($8::text='' OR envelope->'source'->>'ip'=$8) AND ($9::timestamptz IS NULL OR (occurred_at,occurred_at_submicro,event_id)<($9,$10::smallint,$11::uuid)) ORDER BY occurred_at DESC,occurred_at_submicro DESC,event_id DESC LIMIT $12`, org, boundary, nullableID(q.TrapID), q.From, q.To, q.EventType, nullableID(q.SessionID), q.SourceIP, q.AfterTime, submicro(q.AfterTime), nullableID(q.AfterID), q.Limit+1, submicro(q.From), submicro(q.To))
		if err != nil {
			return fmt.Errorf("list events: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			event, err := readEvent(rows)
			if err != nil {
				return err
			}
			items = append(items, event.Summary())
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate events: %w", err)
		}
		return nil
	})
	more := len(items) > q.Limit
	if more {
		items = items[:q.Limit]
	}
	return items, more, boundary, stream, err
}
func nullableID(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func submicro(at *time.Time) int {
	if at == nil {
		return 0
	}
	return at.Nanosecond() % 1000
}
