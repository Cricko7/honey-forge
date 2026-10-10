package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
	"honey-forge/internal/postgres"
)

func testStore(t *testing.T) (*Store, *pgxpool.Pool, context.Context, Scope) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for real PostgreSQL checks")
	}
	pool, err := postgres.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(t.Context(), pool, os.DirFS("../../../../migrations")); err != nil {
		t.Fatal(err)
	}
	org, user := contract.NewID(), contract.NewID()
	ctx := contract.WithPrincipal(t.Context(), contract.Principal{UserID: user, OrganizationID: org, Role: contract.Admin})
	return NewStore(pool), pool, ctx, Scope{OrganizationID: org, Route: "/api/profiles", RequestID: contract.NewID()}
}

func TestDurableIdempotency(t *testing.T) {
	store, pool, ctx, scope := testStore(t)
	id := contract.NewID()
	calls := 0
	write := func(ctx context.Context, tx pgx.Tx) (Outcome, error) {
		calls++
		return Outcome{ResourceID: id, Location: "/api/profiles/" + string(id), Action: "profile.created", Changes: []Change{{Type: "profile.created", ResourceID: id}}}, nil
	}
	first, err := store.Create(ctx, scope, json.RawMessage(`{"name":"x","config":{"a":1,"b":2}}`), write)
	if err != nil || first.Replayed {
		t.Fatalf("%+v %v", first, err)
	}
	restarted := NewStore(pool)
	second, err := restarted.Create(ctx, scope, json.RawMessage(`{"config":{"b":2,"a":1},"name":"x"}`), write)
	if err != nil || !second.Replayed || second.ResourceID != id || calls != 1 {
		t.Fatalf("%+v %v calls %d", second, err, calls)
	}
	_, err = store.Create(ctx, scope, json.RawMessage(`{"name":"changed"}`), write)
	var apiError *contract.Error
	if !errors.As(err, &apiError) || apiError.Code != "idempotency_conflict" {
		t.Fatalf("conflict %v", err)
	}
	if err := store.Delete(ctx, scope.OrganizationID, id, func(context.Context, pgx.Tx) (Outcome, error) {
		return Outcome{ResourceID: id, Action: "profile.deleted", Changes: []Change{{Type: "profile.deleted", ResourceID: id}}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err = store.Create(ctx, scope, json.RawMessage(`{"name":"x","config":{"a":1,"b":2}}`), write)
	if !errors.As(err, &apiError) || apiError.Code != "request_already_used" {
		t.Fatalf("deleted replay %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM mutation_audit WHERE organization_id=$1", string(scope.OrganizationID)).Scan(&count); err != nil || count != 2 {
		t.Fatalf("audit %d %v", count, err)
	}
}

func TestAtomicFailureAndAccess(t *testing.T) {
	store, pool, ctx, scope := testStore(t)
	id := contract.NewID()
	write := func(ctx context.Context, tx pgx.Tx) (Outcome, error) {
		if _, err := tx.Exec(ctx, "INSERT INTO mutation_changes(organization_id,sequence,type,resource_id,metadata) VALUES($1,99,'probe',$2,'{}')", string(scope.OrganizationID), string(id)); err != nil {
			return Outcome{}, err
		}
		return Outcome{ResourceID: id, Location: "/api/profiles/" + string(id), Action: "", Changes: nil}, nil
	}
	if _, err := store.Create(ctx, scope, json.RawMessage(`{}`), write); err == nil {
		t.Fatal("missing audit accepted")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM mutation_changes WHERE organization_id=$1", string(scope.OrganizationID)).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial commit %d %v", count, err)
	}
	for _, tt := range []struct {
		name string
		ctx  context.Context
	}{{"anonymous", t.Context()}, {"viewer", contract.WithPrincipal(t.Context(), contract.Principal{Role: contract.Viewer, OrganizationID: scope.OrganizationID})}, {"foreign", contract.WithPrincipal(t.Context(), contract.Principal{Role: contract.Admin, OrganizationID: contract.NewID()})}} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			_, err := store.Create(tt.ctx, scope, json.RawMessage(`{}`), func(context.Context, pgx.Tx) (Outcome, error) { called = true; return Outcome{}, nil })
			if err == nil || called {
				t.Fatal("unauthorized mutation")
			}
		})
	}
}

func TestConcurrentCreate(t *testing.T) {
	store, pool, ctx, scope := testStore(t)
	id := contract.NewID()
	var wg sync.WaitGroup
	out := make(chan Result, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			r, err := store.Create(ctx, scope, json.RawMessage(`{}`), func(context.Context, pgx.Tx) (Outcome, error) {
				return Outcome{ResourceID: id, Location: "/api/profiles/" + string(id), Action: "profile.created", Changes: []Change{{Type: "profile.created", ResourceID: id}}}, nil
			})
			out <- r
			errs <- err
		})
	}
	wg.Wait()
	close(out)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	fresh := 0
	for r := range out {
		if !r.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("created %d times", fresh)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM mutation_audit WHERE organization_id=$1", string(scope.OrganizationID)).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit %d %v", count, err)
	}
}
