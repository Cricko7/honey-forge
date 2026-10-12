package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"honey-forge/modules/commands"
)

// ExpireAllDue also releases old queues for agents that have never connected.
func (r *Repository) ExpireAllDue(ctx context.Context, now time.Time) error {
	rows, err := r.pool.Query(ctx, `SELECT c.organization_id::text,c.trap_id::text FROM commands c JOIN traps t ON t.id=c.trap_id AND t.organization_id=c.organization_id WHERE c.status IN ('queued','running') AND (c.expires_at<=$1 OR (c.status='queued' AND c.created_at<=$2 AND t.connectivity='offline' AND t.last_seen_at IS NULL))`, now, now.Add(-initialAgentWait))
	if err != nil {
		return fmt.Errorf("find due commands: %w", err)
	}

	type due struct{ organization, trap string }
	var items []due
	for rows.Next() {
		var item due
		if err := rows.Scan(&item.organization, &item.trap); err != nil {
			rows.Close()
			return fmt.Errorf("scan due command: %w", err)
		}

		items = append(items, item)
	}

	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("iterate due commands: %w", err)
	}

	for _, item := range items {
		if err := r.ExpireDue(ctx, item.organization, item.trap, now); err != nil && !errors.Is(err, commands.ErrNotFound) {
			return err
		}
	}

	return nil
}
