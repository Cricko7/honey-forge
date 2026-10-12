//go:build integration

package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/testpostgres"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	return testpostgres.New(t).Migrate(t, "../../../../../migrations")
}

func applyMigrations(t *testing.T, pool *pgxpool.Pool, direction string) {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}

	if direction == "Down" {
		for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
			files[i], files[j] = files[j], files[i]
		}
	}

	for _, file := range files {
		applyMigration(t, pool, file, direction)
	}
}

func applyMigration(t *testing.T, pool *pgxpool.Pool, file, direction string) {
	t.Helper()

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(string(raw), "-- +goose Down")
	sql := strings.TrimPrefix(parts[0], "-- +goose Up")
	if direction == "Down" {
		sql = parts[1]
	}

	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), sql)

		return err
	})
	if err != nil {
		t.Fatalf("migration %s %s: %v", filepath.Base(file), direction, err)
	}
}

func sqlCount(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
		t.Fatal(err)
	}

	return count
}
