//go:build integration

package app

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func createIntegrationTrap(t *testing.T, r *Runtime, a operatorSession) (profiles.Profile, traps.Trap, traps.CreateRequest) {
	t.Helper()
	p := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, catalogProfileRequest(t, r, a)), a, 201))
	req := traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Demo trap", ProfileID: p.ID}
	trap := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, req), a, 201))
	return p, trap, req
}
func trapMutation(t *testing.T, r *Runtime, a operatorSession, method, path, body string, revision int64, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := operatorRequest(t, method, path, body, a)
	req.Header.Set("X-Expected-Revision", fmt.Sprint(revision))
	w := httptest.NewRecorder()
	r.Router.ServeHTTP(w, req)
	if w.Code != status {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	return w
}
func TestTrapsLifecycle(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "traps@example.test", auth.OrganizationInput{Mode: "create", Name: "Traps"})
	p, trap, req := createIntegrationTrap(t, r, admin)
	path := "/api/traps/" + trap.ID
	if trap.Revision != 1 || trap.StateVersion != 1 || trap.Connectivity != "offline" || trap.RuntimeState != "unknown" || trap.DesiredState != "stopped" || trap.Agent != nil || trap.LastSeenAt != nil || trap.AppliedProfileRevision != nil || trap.ActiveCommandID != nil {
		t.Fatalf("initial=%+v", trap)
	}
	patched := decodeIntegration[traps.Trap](t, trapMutation(t, r, admin, "PATCH", path, `{"name":"Renamed"}`, 1, 200))
	if patched.Revision != 2 || patched.StateVersion != 2 {
		t.Fatal(patched)
	}
	replay := sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, req), admin, 200)
	if replay.Header().Get("Idempotency-Replayed") != "true" || decodeIntegration[traps.Trap](t, replay).Name != trap.Name {
		t.Fatal("creation replay changed")
	}
	trapMutation(t, r, admin, "DELETE", path, "", 1, 412)
	trapMutation(t, r, admin, "DELETE", path, "", 2, 204)
	sendOperator(t, r, "GET", path, "", admin, 404)
	trapMutation(t, r, admin, "DELETE", path, "", 2, 404)
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, req), admin, 409)
	sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":0}`, admin, 404)
	// The tombstone releases the profile binding and retains the registration.
	var deleted bool
	if err := r.pool.QueryRow(t.Context(), `SELECT deleted_at IS NOT NULL FROM traps WHERE id=$1`, trap.ID).Scan(&deleted); err != nil || !deleted {
		t.Fatalf("tombstone: %v", err)
	}
	deleteProfile := operatorRequest(t, "DELETE", "/api/profiles/"+p.ID, "", admin)
	deleteProfile.Header.Set("If-Match", profiles.ETag(p))
	w := httptest.NewRecorder()
	r.Router.ServeHTTP(w, deleteProfile)
	if w.Code != 204 {
		t.Fatalf("profile binding: %d %s", w.Code, w.Body.String())
	}
	var audits, changes int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM mutation_audit WHERE resource_id=$1 AND action='trap.deleted'`, trap.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM mutation_changes WHERE resource_id=$1 AND type='trap.deleted'`, trap.ID).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if audits != 1 || changes != 1 {
		t.Fatalf("duplicate effects %d/%d", audits, changes)
	}
}
