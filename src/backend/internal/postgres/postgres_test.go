package postgres

import (
	"context"
	"honey-forge/internal/contract"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFreshConcurrentMigrations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	pool, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := "test_" + strings.ReplaceAll(string(contract.NewID()), "-", "")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(t.Context(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(context.WithoutCancel(t.Context()), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			config, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				errs <- err
				return
			}
			config.ConnConfig.RuntimeParams["search_path"] = schema
			connection, err := pgxpool.NewWithConfig(t.Context(), config)
			if err != nil {
				errs <- err
				return
			}
			defer connection.Close()
			errs <- Migrate(t.Context(), connection, os.DirFS("../../../../migrations"))
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name IN ('mutation_requests','mutation_audit','mutation_changes','organization_changes','schema_versions')", schema).Scan(&count); err != nil || count != 5 {
		t.Fatalf("fresh tables %d %v", count, err)
	}
}
