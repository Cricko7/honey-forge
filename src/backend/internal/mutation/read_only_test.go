package mutation

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
)

func TestGETCannotWriteBusinessState(t *testing.T) {
	org := contract.NewID()
	ctx := contract.WithPrincipal(contract.WithHTTPMethod(t.Context(), "GET"), contract.Principal{UserID: contract.NewID(), OrganizationID: org, Role: contract.Admin})
	store := NewStore(nil)
	called := false
	err := store.Write(ctx, org, func(context.Context, pgx.Tx) (Outcome, error) { called = true; return Outcome{}, nil })
	if err == nil || called {
		t.Fatal("GET changed business state")
	}
}
