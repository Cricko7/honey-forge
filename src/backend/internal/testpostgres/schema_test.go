package testpostgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSearchPathPreservesConnectionSettings(t *testing.T) {
	for _, tc := range []struct{ name, dsn string }{
		{"URL", "postgres://operator:secret@localhost:5432/test?sslmode=verify-full&search_path=public"},
		{"keyword", "host=localhost port=5432 dbname=test user=operator password='secret' sslmode=verify-full search_path=public"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsn, err := withSearchPath(tc.dsn, "test_schema")
			if err != nil {
				t.Fatal(err)
			}

			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}

			if cfg.ConnConfig.RuntimeParams["search_path"] != "test_schema" || cfg.ConnConfig.Database != "test" || cfg.ConnConfig.User != "operator" || cfg.ConnConfig.Password != "secret" || cfg.ConnConfig.TLSConfig == nil || cfg.ConnConfig.TLSConfig.InsecureSkipVerify {
				t.Fatal("schema isolation changed the connection settings")
			}
		})
	}
}
