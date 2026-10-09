package repository

import (
	"context"
	"errors"
	authcore "honey-forge/src/backend/modules/auth"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDatabaseErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause error
		want  error
	}{
		{name: "missing row", cause: pgx.ErrNoRows, want: authcore.ErrNotFound},
		{name: "deadline", cause: context.DeadlineExceeded, want: authcore.ErrUnavailable},
		{name: "canceled", cause: context.Canceled, want: authcore.ErrUnavailable},
		{name: "database shutdown", cause: &pgconn.PgError{Code: "57P01"}, want: authcore.ErrUnavailable},
		{name: "too many connections", cause: &pgconn.PgError{Code: "53300"}, want: authcore.ErrUnavailable},
		{name: "statement timeout", cause: &pgconn.PgError{Code: "57014"}, want: authcore.ErrUnavailable},
		{name: "connection failure", cause: &pgconn.PgError{Code: "08006"}, want: authcore.ErrUnavailable},
		{name: "duplicate email", cause: authcore.ErrEmailTaken, want: authcore.ErrEmailTaken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := databaseError("test query", tt.cause); !errors.Is(got, tt.want) {
				t.Fatalf("classification = %v, want %v", got, tt.want)
			}
		})
	}

	syntax := &pgconn.PgError{Code: "42601"}
	if err := databaseError("invalid SQL", syntax); errors.Is(err, authcore.ErrUnavailable) || !errors.Is(err, syntax) {
		t.Fatal("SQL programming errors must stay internal errors with preserved causes")
	}
}
