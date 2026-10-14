package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultHistoryRetention = 24 * time.Hour

// Bound each transaction so cleanup cannot lock the entire event history.
// Identity, configuration, active commands and idempotent creation receipts stay.
func cleanHistory(ctx context.Context, pool *pgxpool.Pool, now time.Time, retention time.Duration) error {
	cutoff := now.Add(-retention)
	queries := []struct {
		sql  string
		args []any
	}{
		{`WITH locked AS MATERIALIZED (
		 SELECT organization_id FROM organization_changes ORDER BY organization_id FOR UPDATE
		), expired AS (
		 SELECT c.organization_id,c.sequence FROM mutation_changes c JOIN locked l USING(organization_id)
		 WHERE c.created_at<$1 AND c.sequence<COALESCE((SELECT min(n.sequence) FROM mutation_changes n WHERE n.organization_id=c.organization_id AND n.created_at>=$1),9223372036854775807)
		 ORDER BY c.organization_id,c.sequence LIMIT 1000
		), removed AS (
		 DELETE FROM mutation_changes c USING expired e WHERE c.organization_id=e.organization_id AND c.sequence=e.sequence RETURNING c.organization_id,c.sequence
		), updated AS (
		 UPDATE organization_changes o SET stream_floor=GREATEST(o.stream_floor,d.floor)
		 FROM (SELECT organization_id,max(sequence) AS floor FROM removed GROUP BY organization_id) d WHERE o.organization_id=d.organization_id RETURNING o.organization_id
		) SELECT count(*) FROM removed`, []any{cutoff}},
		{`DELETE FROM trap_events WHERE ctid IN (SELECT ctid FROM trap_events WHERE received_at<$1 ORDER BY received_at LIMIT 1000)`, []any{cutoff}},
		// Small deduplication receipts outlive visible history by one day. An
		// unfinished ingestion is never released by the history cleaner.
		{`WITH expired AS MATERIALIZED (
		 SELECT b.trap_id,b.batch_id FROM trap_event_batches b
		 WHERE b.stored_at<$1 AND NOT EXISTS (SELECT 1 FROM trap_ingestions i WHERE i.trap_id=b.trap_id AND i.batch_id=b.batch_id AND NOT i.finished)
		 ORDER BY b.stored_at LIMIT 1000
		), removed AS (
		 DELETE FROM trap_ingestions i USING expired e WHERE i.trap_id=e.trap_id AND i.batch_id=e.batch_id AND i.finished RETURNING i.batch_id
		) DELETE FROM trap_event_batches b USING expired e WHERE b.trap_id=e.trap_id AND b.batch_id=e.batch_id`, []any{cutoff.Add(-24 * time.Hour)}},
		{`DELETE FROM commands WHERE ctid IN (SELECT ctid FROM commands WHERE finished_at<$1 AND expires_at<$2 AND status IN ('succeeded','failed','expired') ORDER BY finished_at LIMIT 1000)`, []any{cutoff, now}},
		{`DELETE FROM auth_audit WHERE ctid IN (SELECT ctid FROM auth_audit WHERE occurred_at<$1 ORDER BY occurred_at LIMIT 1000)`, []any{cutoff}},
		{`DELETE FROM profile_audit WHERE ctid IN (SELECT ctid FROM profile_audit WHERE occurred_at<$1 ORDER BY occurred_at LIMIT 1000)`, []any{cutoff}},
		{`DELETE FROM mutation_audit WHERE ctid IN (SELECT ctid FROM mutation_audit WHERE created_at<$1 ORDER BY created_at LIMIT 1000)`, []any{cutoff}},
		{`DELETE FROM auth_changes WHERE ctid IN (SELECT ctid FROM auth_changes WHERE occurred_at<$1 ORDER BY occurred_at LIMIT 1000)`, []any{cutoff}},
		{`DELETE FROM profile_changes WHERE ctid IN (SELECT ctid FROM profile_changes WHERE occurred_at<$1 ORDER BY occurred_at LIMIT 1000)`, []any{cutoff}},
		{`DELETE FROM operator_sessions WHERE ctid IN (SELECT ctid FROM operator_sessions WHERE expires_at<$1 LIMIT 1000)`, []any{now}},
		{`DELETE FROM http_pipeline_traces WHERE id IN (SELECT id FROM http_pipeline_traces WHERE expires_at<$1 ORDER BY expires_at LIMIT 1000)`, []any{now}},
	}
	for index, query := range queries {
		for range 10 {
			var removed int64
			var err error
			if index == 0 {
				err = pool.QueryRow(ctx, query.sql, query.args...).Scan(&removed)
			} else {
				result, execErr := pool.Exec(ctx, query.sql, query.args...)
				removed, err = result.RowsAffected(), execErr
			}
			if err != nil {
				return fmt.Errorf("clean retained history: %w", err)
			}
			if removed < 1000 {
				break
			}
		}
	}

	return nil
}

func maintainHistory(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, retention time.Duration) {
	if retention == 0 {
		retention = defaultHistoryRetention
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := cleanHistory(work, pool, time.Now(), retention); err != nil && ctx.Err() == nil {
			logger.ErrorContext(work, "history cleanup failed", "error", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
