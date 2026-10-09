//go:build integration

package repository

import (
	"context"
	"errors"
	profilescore "honey-forge/src/backend/modules/profiles"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func profilePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}

	root, e := pgxpool.New(t.Context(), dsn)
	if e != nil {
		t.Fatal(e)
	}

	schema := "profiles_test_" + strings.ReplaceAll(newID(), "-", "")
	if _, e := root.Exec(t.Context(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		root.Close()
		t.Fatal(e)
	}

	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}

	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(t.Context(), cfg)
	if e != nil {
		t.Fatal(e)
	}

	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := root.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}

		root.Close()
	})
	files, e := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "migrations", "*.sql"))
	if e != nil {
		t.Fatal(e)
	}

	for _, file := range files {
		raw, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}

		up := strings.TrimPrefix(strings.Split(string(raw), "-- +goose Down")[0], "-- +goose Up")
		if e := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error { _, e := tx.Exec(t.Context(), up); return e }); e != nil {
			t.Fatalf("migration %s: %v", file, e)
		}
	}

	if _, e := pool.Exec(t.Context(), `INSERT INTO organizations(id,name,join_code) VALUES($1,'Demo','0123456789abcdefghijklmnopqrstuv')`, orgID); e != nil {
		t.Fatal(e)
	}

	if _, e := pool.Exec(t.Context(), `INSERT INTO users(id,organization_id,email,password_hash,role) VALUES($1,$2,'admin@example.com','unused','admin')`, userID, orgID); e != nil {
		t.Fatal(e)
	}

	return pool
}

func TestPostgresProfileLifecycle(t *testing.T) {
	pool := profilePool(t)
	s := integrationService(NewRepository(pool))

	c, e := s.Create(t.Context(), admin, request())
	if e != nil {
		t.Fatal(e)
	}

	p, e := s.Patch(t.Context(), admin, c.Profile.ID, profilescore.ETag(c.Profile), profilescore.PatchRequest{Name: ptr("Changed"), Config: profilescore.Object{"visible": "two"}})
	if e != nil {
		t.Fatal(e)
	}

	r, e := s.Create(t.Context(), admin, request())
	if e != nil || !r.Replayed || r.Current || r.Profile.Name != "Demo" {
		t.Fatalf("replay: %#v %v", r, e)
	}

	page, e := s.List(t.Context(), admin, profilescore.ListOptions{})
	if e != nil || len(page.Items) != 1 || page.Items[0].Revision != 2 {
		t.Fatalf("list: %#v %v", page, e)
	}

	e = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		snap, e := CaptureProfile(t.Context(), tx, orgID, p.ID, 2)
		if e != nil {
			return e
		}

		if !strings.Contains(mustJSON(snap), "private") {
			t.Fatal("snapshot lost secret")
		}

		_, e = CaptureProfile(t.Context(), tx, orgID, p.ID, 1)
		if !errors.Is(e, profilescore.ErrProfileChanged) {
			t.Fatalf("capture stale: %v", e)
		}

		return nil
	})
	if e != nil {
		t.Fatal(e)
	}

	if e := s.Delete(t.Context(), admin, p.ID, profilescore.ETag(p)); e != nil {
		t.Fatal(e)
	}

	if _, e := s.ReadProfile(t.Context(), admin, p.ID); !errors.Is(e, profilescore.ErrNotFound) {
		t.Fatalf("deleted get: %v", e)
	}

	if _, e := s.ReadSnapshot(t.Context(), orgID, p.ID, 1); e != nil {
		t.Fatal(e)
	}

	if _, e := s.Create(t.Context(), admin, request()); !errors.Is(e, profilescore.ErrRequestUsed) {
		t.Fatalf("deleted replay: %v", e)
	}

	var audits, changes int
	if e := pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM profile_audit),(SELECT count(*) FROM profile_changes)`).Scan(&audits, &changes); e != nil {
		t.Fatal(e)
	}

	if audits != 3 || changes != 6 {
		t.Fatalf("audit/change count: %d/%d", audits, changes)
	}
}

func TestPostgresProfileConcurrencyAndRollback(t *testing.T) {
	pool := profilePool(t)
	repo := NewRepository(pool)
	s := integrationService(repo)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, e := s.Create(t.Context(), admin, request()); results <- e })
	}

	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}

	page, e := s.List(t.Context(), admin, profilescore.ListOptions{})
	if e != nil || len(page.Items) != 1 {
		t.Fatalf("duplicates: %#v %v", page, e)
	}

	p := page.Items[0]
	results = make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, e := s.Patch(t.Context(), admin, p.ID, profilescore.ETag(p), profilescore.PatchRequest{Name: ptr(newID())})
			results <- e
		})
	}

	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if !errors.Is(e, profilescore.ErrRevisionMismatch) {
			t.Fatal(e)
		}
	}

	if wins != 1 {
		t.Fatalf("patch winners: %d", wins)
	}

	repo.writers.AppendChange = func(context.Context, pgx.Tx, string, string, profilescore.Object) error {
		return errors.New("journal offline")
	}
	req := request()
	req.RequestID = userID
	if _, e := s.Create(t.Context(), admin, req); e == nil {
		t.Fatal("journal failure accepted")
	}

	var profiles, audits int
	if e := pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM profiles),(SELECT count(*) FROM profile_audit)`).Scan(&profiles, &audits); e != nil {
		t.Fatal(e)
	}

	if profiles != 1 || audits != 2 {
		t.Fatalf("partial commit: %d/%d", profiles, audits)
	}
}
