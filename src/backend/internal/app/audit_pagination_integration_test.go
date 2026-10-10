//go:build integration

package app

import (
	"honey-forge/internal/contract"
	"honey-forge/modules/audit"
	"honey-forge/modules/auth"
	"honey-forge/modules/traps"
	"testing"
)

func TestAuditPaginationExcludesLateCommitWithEarlierTimestamp(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "audit-pages@example.test", auth.OrganizationInput{Mode: "create", Name: "Pages"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	first := decodeIntegration[contract.Page[audit.Entry]](t, sendOperator(t, r, "GET", "/api/audit-entries?limit=1", "", a, 200))
	created := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Late", ProfileID: trap.ProfileID}), a, 201))
	// Model a late commit with a timestamp older than the captured REST snapshot.
	if _, err := r.pool.Exec(t.Context(), `UPDATE mutation_audit SET created_at='2000-01-01' WHERE resource_id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	total := len(first.Items)
	next := first.NextCursor
	for next != nil {
		page := decodeIntegration[contract.Page[audit.Entry]](t, sendOperator(t, r, "GET", "/api/audit-entries?limit=1&cursor="+*next, "", a, 200))
		for _, entry := range page.Items {
			if entry.Resource.ID == created.ID {
				t.Fatal("late commit entered old page snapshot")
			}
		}
		total += len(page.Items)
		next = page.NextCursor
	}
	if total != 4 {
		t.Fatalf("original snapshot has %d entries, want 4", total)
	}
}
