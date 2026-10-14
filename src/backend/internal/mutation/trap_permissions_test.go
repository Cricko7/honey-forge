package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"

	"honey-forge/internal/contract"
)

func TestDelegatedMutationStoreStaysWithinTrapBoundary(t *testing.T) {
	org := contract.NewID()
	ctx := contract.WithPrincipal(t.Context(), contract.Principal{UserID: contract.NewID(), OrganizationID: org, Role: contract.Viewer, CanManageTraps: true})
	for _, tc := range []struct {
		name  string
		store *Store
		want  string
	}{
		{"default rejects delegate", NewStore(nil), "forbidden"},
		{"trap store reaches validation", NewTrapStore(nil), "validation_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.store.Create(ctx, Scope{OrganizationID: org}, json.RawMessage(`{}`), func(context.Context, pgx.Tx) (Outcome, error) {
				t.Fatal("unauthorized or invalid request reached storage")
				return Outcome{}, nil
			})
			var api *contract.Error
			if !errors.As(err, &api) || api.Code != tc.want {
				t.Fatalf("error=%v want=%s", err, tc.want)
			}
		})
	}
}
