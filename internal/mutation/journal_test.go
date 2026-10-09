package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"honey-forge/internal/contract"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestJournalReplayAndSnapshot(t *testing.T) {
	store, _, ctx, scope := testStore(t)
	id := contract.NewID()
	result, err := store.Create(ctx, scope, json.RawMessage(`{}`), func(context.Context, pgx.Tx) (Outcome, error) {
		return Outcome{ResourceID: id, Location: "/api/profiles/" + string(id), Action: "profile.created", Changes: []Change{{Type: "profile.created", ResourceID: id}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := store.Snapshot(ctx, scope.OrganizationID, func(ctx context.Context, tx pgx.Tx) error {
		var count int
		return tx.QueryRow(ctx, "SELECT count(*) FROM mutation_requests WHERE organization_id=$1", string(scope.OrganizationID)).Scan(&count)
	})
	if err != nil || boundary != result.StreamSequence {
		t.Fatalf("snapshot %d %v", boundary, err)
	}
	changes, err := store.Changes(ctx, scope.OrganizationID, 0, boundary, 50)
	if err != nil || len(changes) != 1 || changes[0].ResourceID != id {
		t.Fatalf("replay %+v %v", changes, err)
	}
	if _, err := store.Changes(ctx, contract.NewID(), 0, boundary, 50); err == nil {
		t.Fatal("foreign journal exposed")
	}
	if _, err := store.Changes(ctx, scope.OrganizationID, 0, boundary, 101); err == nil {
		t.Fatal("invalid limit accepted")
	}
}

func TestAtomicRevision(t *testing.T) {
	store, pool, ctx, scope := testStore(t)
	if _, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS test_revisions(id uuid PRIMARY KEY,revision bigint NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	id := contract.NewID()
	if _, err := pool.Exec(ctx, "INSERT INTO test_revisions VALUES($1,1)", string(id)); err != nil {
		t.Fatal(err)
	}
	read := func(ctx context.Context, tx pgx.Tx) (contract.Revision, error) {
		var r contract.Revision
		err := tx.QueryRow(ctx, "SELECT revision FROM test_revisions WHERE id=$1 FOR UPDATE", string(id)).Scan(&r)
		return r, err
	}
	write := func(ctx context.Context, tx pgx.Tx, next contract.Revision) (Outcome, error) {
		if _, err := tx.Exec(ctx, "UPDATE test_revisions SET revision=$2 WHERE id=$1", string(id), next); err != nil {
			return Outcome{}, err
		}
		return Outcome{ResourceID: id, Action: "profile.updated", Metadata: Metadata{Revision: &next}, Changes: []Change{{Type: "profile.changed", ResourceID: id, Metadata: Metadata{Revision: &next}}}}, nil
	}
	if err := store.UpdateRevision(ctx, scope.OrganizationID, 1, read, write); err != nil {
		t.Fatal(err)
	}
	err := store.UpdateRevision(ctx, scope.OrganizationID, 1, read, write)
	var apiError *contract.Error
	if !errors.As(err, &apiError) || apiError.Code != "revision_mismatch" {
		t.Fatalf("stale revision %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE test_revisions SET revision=$2 WHERE id=$1", string(id), contract.MaxRevision); err != nil {
		t.Fatal(err)
	}
	err = store.UpdateRevision(ctx, scope.OrganizationID, contract.MaxRevision, read, write)
	if !errors.As(err, &apiError) || apiError.Code != "revision_exhausted" {
		t.Fatalf("overflow %v", err)
	}
}
