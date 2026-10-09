package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"honey-forge/internal/contract"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool} }

type WriteFunc func(context.Context, pgx.Tx) (Outcome, error)

func (s *Store) Create(ctx context.Context, scope Scope, normalized json.RawMessage, write WriteFunc) (Result, error) {
	if err := authorize(ctx, scope.OrganizationID); err != nil {
		return Result{}, err
	}
	if !contract.ValidID(string(scope.RequestID)) || scope.Route == "" || !strings.HasPrefix(scope.Route, "/api/") || strings.ContainsAny(scope.Route, "?#") || scope.TrapID != "" && !contract.ValidID(string(scope.TrapID)) {
		return Result{}, contract.NewError("validation_failed")
	}
	hash, err := Fingerprint(normalized)
	if err != nil {
		return Result{}, err
	}
	var result Result
	err = s.transaction(ctx, scope.OrganizationID, func(tx pgx.Tx) error {
		var previous []byte
		var deleted bool
		var id, location *string
		var sequence *int64
		err := tx.QueryRow(ctx, `SELECT fingerprint,resource_id::text,location,stream_sequence,deleted FROM mutation_requests WHERE organization_id=$1 AND route=$2 AND trap_scope=$3 AND request_id=$4`, string(scope.OrganizationID), scope.Route, string(scope.TrapID), string(scope.RequestID)).Scan(&previous, &id, &location, &sequence, &deleted)
		if err == nil {
			if !equalFingerprint(previous, hash[:]) {
				return contract.NewError("idempotency_conflict")
			}
			if deleted {
				return contract.NewError("request_already_used")
			}
			if id == nil || location == nil || sequence == nil {
				return fmt.Errorf("incomplete idempotency record")
			}
			result = Result{ResourceID: contract.ID(*id), Location: *location, StreamSequence: *sequence, Replayed: true}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read idempotency record: %w", err)
		}
		outcome, err := write(ctx, tx)
		if err != nil {
			return err
		}
		if err := validateOutcome(outcome, true); err != nil {
			return err
		}
		sequenceValue, err := record(ctx, tx, scope.OrganizationID, outcome)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mutation_requests(organization_id,route,trap_scope,request_id,fingerprint,resource_id,location,stream_sequence) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, string(scope.OrganizationID), scope.Route, string(scope.TrapID), string(scope.RequestID), hash[:], string(outcome.ResourceID), outcome.Location, sequenceValue); err != nil {
			return fmt.Errorf("retain idempotency key: %w", err)
		}
		result = Result{ResourceID: outcome.ResourceID, Location: outcome.Location, StreamSequence: sequenceValue}
		return nil
	})
	return result, err
}

func (s *Store) Write(ctx context.Context, organizationID contract.ID, write WriteFunc) error {
	if err := authorize(ctx, organizationID); err != nil {
		return err
	}
	return s.transaction(ctx, organizationID, func(tx pgx.Tx) error {
		outcome, err := write(ctx, tx)
		if err != nil {
			return err
		}
		if err := validateOutcome(outcome, false); err != nil {
			return err
		}
		_, err = record(ctx, tx, organizationID, outcome)
		return err
	})
}
func (s *Store) Delete(ctx context.Context, organizationID, resourceID contract.ID, write WriteFunc) error {
	if err := authorize(ctx, organizationID); err != nil {
		return err
	}
	if !contract.ValidID(string(resourceID)) {
		return contract.NewError("invalid_id")
	}
	return s.transaction(ctx, organizationID, func(tx pgx.Tx) error {
		outcome, err := write(ctx, tx)
		if err != nil {
			return err
		}
		if outcome.ResourceID != resourceID {
			return fmt.Errorf("delete resource mismatch")
		}
		if err := validateOutcome(outcome, false); err != nil {
			return err
		}
		if _, err := record(ctx, tx, organizationID, outcome); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mutation_requests SET deleted=true WHERE organization_id=$1 AND resource_id=$2`, string(organizationID), string(resourceID)); err != nil {
			return fmt.Errorf("retain deleted resource marker: %w", err)
		}
		return nil
	})
}

func authorize(ctx context.Context, organizationID contract.ID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := contract.AuthorizeCapability(ctx, contract.WriteResources); err != nil {
		return err
	}
	if contract.ReadOnlyRequest(ctx) {
		return contract.NewError("forbidden")
	}
	if err := contract.RequireOrganization(ctx, organizationID); err != nil {
		return err
	}
	if !contract.ValidID(string(organizationID)) {
		return contract.NewError("invalid_id")
	}
	p, _ := contract.PrincipalFrom(ctx)
	if !contract.ValidID(string(p.UserID)) {
		return contract.NewError("unauthenticated")
	}
	return nil
}
func (s *Store) transaction(ctx context.Context, organizationID contract.ID, fn func(pgx.Tx) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin mutation: %w", err)
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if rollbackErr := tx.Rollback(rollback); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback mutation: %w", rollbackErr))
		}
	}()
	if _, err := tx.Exec(ctx, `INSERT INTO organization_changes(organization_id) VALUES($1) ON CONFLICT DO NOTHING`, string(organizationID)); err != nil {
		return fmt.Errorf("initialize organization journal: %w", err)
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT sequence FROM organization_changes WHERE organization_id=$1 FOR UPDATE`, string(organizationID)).Scan(&sequence); err != nil {
		return fmt.Errorf("lock organization journal: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit mutation: %w", err)
	}
	return nil
}

func validateOutcome(outcome Outcome, creation bool) error {
	if !contract.ValidID(string(outcome.ResourceID)) || !contract.ValidTypeID(outcome.Action) || len(outcome.Changes) == 0 {
		return fmt.Errorf("mutation requires a resource, audit and notification")
	}
	if creation {
		u, err := url.Parse(outcome.Location)
		if err != nil || !strings.HasPrefix(u.Path, "/api/") || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("creation requires a GET resource location")
		}
	}
	for _, change := range outcome.Changes {
		if !contract.ValidTypeID(change.Type) || !contract.ValidID(string(change.ResourceID)) {
			return fmt.Errorf("invalid change metadata")
		}
	}
	return nil
}
