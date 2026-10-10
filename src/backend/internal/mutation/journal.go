package mutation

import (
	"context"
	"errors"
	"fmt"
	"honey-forge/internal/contract"
	"time"

	"github.com/jackc/pgx/v5"
)

func readAccess(ctx context.Context, organizationID contract.ID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := contract.AuthorizeCapability(ctx, contract.ReadResources); err != nil {
		return err
	}
	return contract.RequireOrganization(ctx, organizationID)
}

// Snapshot returns the journal boundary observed in the same repeatable-read
// transaction as the REST read. New commits cannot move this page boundary.
func (s *Store) Snapshot(ctx context.Context, organizationID contract.ID, read func(context.Context, pgx.Tx) error) (sequence int64, err error) {
	if err := readAccess(ctx, organizationID); err != nil {
		return 0, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return 0, fmt.Errorf("begin snapshot: %w", err)
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if e := tx.Rollback(rollback); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback snapshot: %w", e))
		}
	}()
	err = tx.QueryRow(ctx, "SELECT sequence FROM organization_changes WHERE organization_id=$1", string(organizationID)).Scan(&sequence)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("read snapshot boundary: %w", err)
	}
	if err := read(ctx, tx); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit snapshot: %w", err)
	}
	return sequence, nil
}

func (s *Store) Changes(ctx context.Context, organizationID contract.ID, after, boundary int64, limit int) ([]RecordedChange, error) {
	if err := readAccess(ctx, organizationID); err != nil {
		return nil, err
	}
	if after < 0 || boundary < after || limit < 1 || limit > 100 {
		return nil, contract.NewError("invalid_query")
	}
	rows, err := s.pool.Query(ctx, `SELECT sequence,type,resource_id::text,metadata,created_at FROM mutation_changes WHERE organization_id=$1 AND sequence>$2 AND sequence<=$3 ORDER BY sequence LIMIT $4`, string(organizationID), after, boundary, limit)
	if err != nil {
		return nil, fmt.Errorf("read change journal: %w", err)
	}
	defer rows.Close()
	changes := []RecordedChange{}
	for rows.Next() {
		var change RecordedChange
		var created time.Time
		if err := rows.Scan(&change.Sequence, &change.Type, &change.ResourceID, &change.Metadata, &created); err != nil {
			return nil, fmt.Errorf("decode change: %w", err)
		}
		change.CreatedAt = contract.Timestamp(created)
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate changes: %w", err)
	}
	return changes, nil
}

// Both the revision check and the feature/lifecycle write run under the same
// organization transaction. The feature read must include organization scope.
func (s *Store) UpdateRevision(ctx context.Context, organizationID contract.ID, expected contract.Revision, read func(context.Context, pgx.Tx) (contract.Revision, error), write func(context.Context, pgx.Tx, contract.Revision) (Outcome, error)) error {
	return s.Write(ctx, organizationID, func(ctx context.Context, tx pgx.Tx) (Outcome, error) {
		current, err := read(ctx, tx)
		if err != nil {
			return Outcome{}, err
		}
		if current != expected {
			return Outcome{}, contract.NewError("revision_mismatch")
		}
		next, err := contract.NextRevision(current)
		if err != nil {
			return Outcome{}, err
		}
		return write(ctx, tx, next)
	})
}
