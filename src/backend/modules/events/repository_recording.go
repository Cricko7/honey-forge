package events

import (
	"context"
	"fmt"

	"honey-forge/internal/contract"
)

func (r *Repository) Recording(ctx context.Context, trapID, sessionID string) ([]Event, error) {
	org, err := readAccess(ctx)
	if err != nil {
		return nil, err
	}
	visible, err := r.traps.Visible(ctx, org, trapID)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, contract.NewError("resource_not_found")
	}
	// The runtime emits at most 8192 events plus its final connection_closed.
	rows, err := r.pool.Query(ctx, `SELECT envelope,trap_id::text,received_at,event_data FROM trap_events
		WHERE organization_id=$1 AND trap_id=$2 AND session_id=$3 AND envelope->>'type_id'='ssh-high-interaction'
		ORDER BY session_sequence,event_id LIMIT 8193`, org, trapID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query SSH recording: %w", err)
	}
	defer rows.Close()
	items := []Event{}
	for rows.Next() {
		e, err := readEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read SSH recording: %w", err)
	}
	return items, nil
}
