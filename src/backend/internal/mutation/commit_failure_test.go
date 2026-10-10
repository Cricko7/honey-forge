package mutation

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
)

func TestNotificationWriteFailureRollsBackAudit(t *testing.T) {
	store, pool, ctx, scope := testStore(t)
	id := contract.NewID()
	_, err := store.Create(ctx, scope, json.RawMessage(`{}`), func(ctx context.Context, tx pgx.Tx) (Outcome, error) {
		if _, err := tx.Exec(ctx, "UPDATE organization_changes SET sequence=$2 WHERE organization_id=$1", string(scope.OrganizationID), int64(math.MaxInt64)); err != nil {
			return Outcome{}, err
		}
		return Outcome{ResourceID: id, Location: "/api/profiles/" + string(id), Action: "profile.created", Changes: []Change{{Type: "profile.created", ResourceID: id}}}, nil
	})
	if err == nil {
		t.Fatal("notification failure was acknowledged")
	}
	for _, table := range []string{"mutation_requests", "mutation_audit", "mutation_changes", "organization_changes"} {
		var count int
		query := "SELECT count(*) FROM " + pgx.Identifier{table}.Sanitize() + " WHERE organization_id=$1"
		if err := pool.QueryRow(ctx, query, string(scope.OrganizationID)).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial state in %s: %d %v", table, count, err)
		}
	}
}
