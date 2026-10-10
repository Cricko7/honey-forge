//go:build integration

package app

import (
	"net/http/httptest"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	"honey-forge/modules/traps"
)

func TestTrapAccessAndCredentialPreconditions(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "access@example.test", auth.OrganizationInput{Mode: "create", Name: "Access"})
	join := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, r, "viewertrap@example.test", auth.OrganizationInput{Mode: "join", JoinCode: join.Code})
	foreign := registerOperator(t, r, "othertrap@example.test", auth.OrganizationInput{Mode: "create", Name: "Other"})
	_, trap, _ := createIntegrationTrap(t, r, admin)
	path := "/api/traps/" + trap.ID
	for _, method := range []string{"GET", "POST", "DELETE"} {
		sendOperator(t, r, method, path+"/agent-credentials", ``, viewer, 403)
		sendOperator(t, r, method, path+"/agent-credentials", ``, foreign, 404)
	}
	sendOperator(t, r, "GET", path, "", viewer, 200)
	sendOperator(t, r, "GET", path, "", foreign, 404)
	sendOperator(t, r, "PATCH", path, `{}`, viewer, 403)
	sendOperator(t, r, "DELETE", path, "", viewer, 403)
	// Real session middleware verifies CSRF before the feature is reached.
	missing := operatorRequest(t, "POST", path+"/agent-credentials", `{"expected_generation":0}`, admin)
	missing.Header.Del("X-CSRF-Token")
	w := httptest.NewRecorder()
	r.Router.ServeHTTP(w, missing)
	if w.Code != 403 {
		t.Fatalf("CSRF=%d", w.Code)
	}
	status := sendOperator(t, r, "GET", path+"/agent-credentials", "", admin, 200)
	if status.Header().Get("ETag") != traps.CredentialsETag(trap.ID, 0) {
		t.Fatal("missing initial ETag")
	}
	first := sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":0}`, admin, 200)
	sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":0}`, admin, 409)
	sendOperator(t, r, "DELETE", path+"/agent-credentials", "", admin, 428)
	sendOperator(t, r, "DELETE", path+"/agent-credentials", "", admin, 400, "*")
	sendOperator(t, r, "DELETE", path+"/agent-credentials", "", admin, 204, first.Header().Get("ETag"))
	revoked := decodeIntegration[traps.CredentialsStatus](t, sendOperator(t, r, "GET", path+"/agent-credentials", "", admin, 200))
	if revoked.Generation != 2 || revoked.Active || revoked.IssuedAt == nil {
		t.Fatal(revoked)
	}
	sendOperator(t, r, "DELETE", path+"/agent-credentials", "", admin, 409, first.Header().Get("ETag"))
	sendOperator(t, r, "DELETE", path+"/agent-credentials", "", admin, 204, traps.CredentialsETag(trap.ID, 2))
	// credentials ever issued means the disconnected-new-trap exception is gone.
	trapMutation(t, r, admin, "DELETE", path, "", 1, 409)
	empty := sendOperator(t, r, "GET", "/api/traps?connectivity=online", "", viewer, 200)
	if len(decodeIntegration[contract.Page[traps.Trap]](t, empty).Items) != 0 {
		t.Fatal("revoked trap online")
	}
}
func TestTrapConnectionFencingExpiryAndOverflow(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "expiretrap@example.test", auth.OrganizationInput{Mode: "create", Name: "Expiry"})
	_, trap, _ := createIntegrationTrap(t, r, admin)
	path := "/api/traps/" + trap.ID
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, admin, trap)
	replacement := string(contract.NewID())
	// A second accepted hello fences the previous connection's heartbeat/offline.
	_, err := r.Agents.Hello(ctx, identity, replacement, agentHello(runtime, "0.2", "new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Agents.Observe(ctx, identity, connection, runtime); err == nil {
		t.Fatal("stale heartbeat accepted")
	}
	if err := r.Agents.Offline(ctx, identity, connection); err != nil {
		t.Fatal(err)
	}
	before := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", admin, 200))
	if before.Connectivity != "online" {
		t.Fatal("stale disconnect won")
	}
	if _, err := r.Agents.Report(ctx, identity, replacement, runtime); err != nil {
		t.Fatal(err)
	}
	reported := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", admin, 200))
	if !reported.LastSeenAt.Equal(*before.LastSeenAt) || reported.StateVersion != before.StateVersion {
		t.Fatal("result refreshed heartbeat")
	}
	if _, err := r.pool.Exec(t.Context(), `UPDATE traps SET last_seen_at=$2 WHERE id=$1`, trap.ID, time.Now().Add(-31*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := r.Agents.ExpireConnections(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	offline := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", admin, 200))
	if offline.Connectivity != "offline" || offline.RuntimeState != "stopped" || offline.Revision != 1 || offline.StateVersion != before.StateVersion+1 {
		t.Fatal(offline)
	}
	_, err = r.Agents.Hello(ctx, identity, replacement, agentHello(runtime, "0.2", "new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(t.Context(), `UPDATE traps SET state_version=2147483647 WHERE id=$1`, trap.ID); err != nil {
		t.Fatal(err)
	}
	before = decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", admin, 200))
	runtime.BufferedEvents = 1
	if _, err := r.Agents.Observe(ctx, identity, replacement, runtime); err == nil {
		t.Fatal("version overflow accepted")
	}
	sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":1}`, admin, 409)
	after := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", admin, 200))
	if after.Agent.BufferedEvents != before.Agent.BufferedEvents || !after.LastSeenAt.Equal(*before.LastSeenAt) || after.Connectivity != "online" {
		t.Fatal("overflow partially wrote")
	}
	status := decodeIntegration[traps.CredentialsStatus](t, sendOperator(t, r, "GET", path+"/agent-credentials", "", admin, 200))
	if status.Generation != 1 {
		t.Fatal("overflow rotated token")
	}
	// Invalid applied revisions never become runtime observations.
	unknown := int32(10)
	runtime.AppliedProfileRevision = &unknown
	if _, err := r.Agents.Observe(ctx, identity, replacement, runtime); err == nil {
		t.Fatal("unknown config accepted")
	}
}
func agentHello(runtime commands.AgentRuntime, version, hostname string) agentws.AgentHello {
	return agentws.AgentHello{AgentVersion: version, Hostname: hostname, Runtime: runtime}
}
