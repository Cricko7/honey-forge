// Package testpostgres owns isolated PostgreSQL schemas for integration tests.
package testpostgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/postgres"
)

type Schema struct {
	Pool *pgxpool.Pool
	DSN  string
}

// New registers cleanup immediately after acquiring each resource. Cleanup has
// its own deadline because the test context is already canceled at that point.
func New(t *testing.T) *Schema {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for PostgreSQL integration tests")
	}
	root, err := postgres.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "test_" + hex.EncodeToString(random[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := root.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := root.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
	})
	schemaDSN, err := withSearchPath(dsn, name)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.Open(t.Context(), schemaDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &Schema{Pool: pool, DSN: schemaDSN}
}

func (s *Schema) Migrate(t *testing.T, directory string) *pgxpool.Pool {
	t.Helper()
	if err := postgres.Migrate(t.Context(), s.Pool, os.DirFS(directory)); err != nil {
		t.Fatal(err)
	}
	return s.Pool
}

func withSearchPath(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		query := u.Query()
		query.Set("search_path", name)
		u.RawQuery = query.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + name
	}
	// Validate both pgx DSN formats before attempting another connection.
	_, err := pgxpool.ParseConfig(dsn)
	return dsn, err
}
