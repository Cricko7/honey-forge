//go:build integration

package catalog

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
	"honey-forge/internal/postgres"
)

func TestRepositoryInstall(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	base, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	name := "catalog_test_" + strings.ReplaceAll(string(contract.NewID()), "-", "")
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := base.Exec(t.Context(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := base.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := postgres.Migrate(t.Context(), pool, os.DirFS("../../../../migrations")); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(pool)
	s := testService(t)
	if err := repo.Install(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if err := repo.Install(t.Context(), s); err != nil {
		t.Fatalf("same definition rejected: %v", err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := repo.Install(t.Context(), s); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	changed := BuiltinDefinitions()
	changed[0].Entry.ConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`)
	if err := repo.Install(t.Context(), testService(t, changed...)); err == nil {
		t.Fatal("changed immutable version accepted")
	}
	additional := BuiltinDefinitions()[0]
	additional.Entry.TypeID = "aaa"
	if err := repo.Install(t.Context(), testService(t, append(changed, additional)...)); err == nil {
		t.Fatal("changed version accepted with a new entry")
	}
	var partial int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM catalog_versions WHERE type_id='aaa'").Scan(&partial); err != nil || partial != 0 {
		t.Fatalf("partial installation committed: %d %v", partial, err)
	}
	if err := repo.Install(t.Context(), testService(t, []Definition{}...)); err == nil {
		t.Fatal("old version removed")
	}
	second := BuiltinDefinitions()[0]
	second.Entry.TypeVersion = 2
	if err := repo.Install(t.Context(), testService(t, append(BuiltinDefinitions(), second)...)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM catalog_versions").Scan(&count); err != nil || count != 3 {
		t.Fatalf("versions: %d %v", count, err)
	}
	for _, query := range []string{"UPDATE catalog_versions SET entry='{}'::jsonb", "DELETE FROM catalog_versions"} {
		if _, err := pool.Exec(t.Context(), query); err == nil {
			t.Fatal("database allowed immutable version mutation")
		}
	}
}
