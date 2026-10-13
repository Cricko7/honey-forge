//go:build integration

package app

import (
	"os"
	"strings"
	"sync"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func TestTrapDeletionCleansOnlyItsLastProfileBinding(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "cleanup@example.test", auth.OrganizationInput{Mode: "create", Name: "Cleanup"})
	join := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, r, "cleanup-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: join.Code})
	foreign := registerOperator(t, r, "cleanup-foreign@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	p, first, _ := createIntegrationTrap(t, r, admin)
	other, _, _ := createIntegrationTrap(t, r, foreign)
	req := traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Shared profile", ProfileID: p.ID}
	second := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, req), admin, 201))
	path := "/api/traps/" + first.ID
	trapMutation(t, r, viewer, "DELETE", path, "", 1, 403)
	trapMutation(t, r, foreign, "DELETE", path, "", 1, 404)
	trapMutation(t, r, admin, "DELETE", path, "", 2, 412)
	sendOperator(t, r, "GET", "/api/profiles/"+p.ID, "", admin, 200)
	trapMutation(t, r, admin, "DELETE", path, "", 1, 204)
	sendOperator(t, r, "GET", "/api/profiles/"+p.ID, "", admin, 200)
	trapMutation(t, r, admin, "DELETE", "/api/traps/"+second.ID, "", 1, 204)
	sendOperator(t, r, "GET", "/api/profiles/"+p.ID, "", admin, 404)
	sendOperator(t, r, "GET", "/api/profiles/"+other.ID, "", foreign, 200)
	var audits, changes int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM profile_audit WHERE resource_id=$1 AND action='profile.deleted'`, p.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}

	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM mutation_changes WHERE resource_id=$1 AND type='profile.deleted'`, p.ID).Scan(&changes); err != nil {
		t.Fatal(err)
	}

	if audits != 1 || changes != 1 {
		t.Fatalf("profile deletion audit/notification = %d/%d", audits, changes)
	}
}

func TestConcurrentTrapDeletionsCleanSharedProfile(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "concurrent-cleanup@example.test", auth.OrganizationInput{Mode: "create", Name: "Concurrent cleanup"})
	p, first, _ := createIntegrationTrap(t, r, admin)
	req := traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Shared", ProfileID: p.ID}
	second := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, req), admin, 201))
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, trap := range []traps.Trap{first, second} {
		request := operatorRequest(t, "DELETE", "/api/traps/"+trap.ID, "", admin)
		request.Header.Set("X-Expected-Revision", "1")
		wg.Go(func() { codes <- serveOperator(r.Router, request).Code })
	}

	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 204 {
			t.Fatalf("concurrent deletion: %d", code)
		}
	}

	sendOperator(t, r, "GET", "/api/profiles/"+p.ID, "", admin, 404)
}

func TestProfileCleanupMigrationRemovesOnlyUnboundProfiles(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "migration-cleanup@example.test", auth.OrganizationInput{Mode: "create", Name: "Cleanup migration"})
	bound, _, _ := createIntegrationTrap(t, r, admin)
	orphan, oldTrap, _ := createIntegrationTrap(t, r, admin)
	unbound := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, catalogProfileRequest(t, r, admin)), admin, 201))
	// Simulate registrations deleted by the old backend before deploying the update.
	if _, err := r.pool.Exec(t.Context(), `UPDATE traps SET deleted_at=clock_timestamp() WHERE id=$1`, oldTrap.ID); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile("../../../../migrations/20261013000300_cleanup_unbound_profiles.sql")
	if err != nil {
		t.Fatal(err)
	}

	up, _, _ := strings.Cut(string(raw), "-- +goose Down")
	for range 2 {
		if _, err := r.pool.Exec(t.Context(), up); err != nil {
			t.Fatal(err)
		}
	}

	sendOperator(t, r, "GET", "/api/profiles/"+bound.ID, "", admin, 200)
	for _, p := range []profiles.Profile{orphan, unbound} {
		sendOperator(t, r, "GET", "/api/profiles/"+p.ID, "", admin, 404)
		var changes int
		if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM profile_changes WHERE type='profile.deleted' AND data->>'profile_id'=$1`, p.ID).Scan(&changes); err != nil || changes != 1 {
			t.Fatalf("cleanup notifications=%d err=%v", changes, err)
		}
	}
}
