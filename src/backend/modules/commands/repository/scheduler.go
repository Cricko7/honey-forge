package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"honey-forge/modules/commands"
)

// ExpireAllDue keeps offline queues within their 24-hour lifetime even when
// neither an operator nor an agent makes another request.
func (r *Repository) ExpireAllDue(ctx context.Context, now time.Time) error {
	rows, err := r.pool.Query(ctx, `SELECT organization_id::text,trap_id::text FROM commands WHERE status IN ('queued','running') AND expires_at<=$1`, now)
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
