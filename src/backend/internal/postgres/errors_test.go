package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConnectionErrorsDoNotExposeSecrets(t *testing.T) {
	_, err := Open(t.Context(), "postgres://operator:server-secret@localhost:invalid/database")
	if err == nil || strings.Contains(err.Error(), "server-secret") {
		t.Fatalf("unsafe database error %v", err)
	}
}

func TestUnavailableErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"deadline", fmt.Errorf("query: %w", context.DeadlineExceeded), true},
		{"shutdown", &pgconn.PgError{Code: "57P01"}, true},
		{"connections", &pgconn.PgError{Code: "53300"}, true},
		{"constraint", &pgconn.PgError{Code: "23503"}, false},
		{"domain", errors.New("not found"), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if IsUnavailable(tt.err) != tt.want {
				t.Fatal(tt.err)
			}
		})
	}
}
