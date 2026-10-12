//go:build integration

package app

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func TestManagedTrapPortConflictIsAtomicAcrossOrganizations(t *testing.T) {
	r := openIntegrationRuntime(t)
	admins := []operatorSession{
		registerOperator(t, r, "ports-a@example.test", auth.OrganizationInput{Mode: "create", Name: "A"}),
		registerOperator(t, r, "ports-b@example.test", auth.OrganizationInput{Mode: "create", Name: "B"}),
	}
	requests := make([]traps.CreateRequest, 2)
	for i, admin := range admins {
		p := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, catalogProfileRequest(t, r, admin)), admin, 201))
		requests[i] = traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Port test", ProfileID: p.ID}
	}
	// Manually deployed traps have independent hosts and do not occupy this host.
	manual := requests[0]
	manual.RequestID = string(contract.NewID())
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, manual), admins[0], 201)
	r.Traps.WithAutoDeploy(true)
	join := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admins[0], 200))
	viewer := registerOperator(t, r, "ports-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: join.Code})
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, requests[0]), viewer, 403)

	responses := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i, admin := range admins {
		req := operatorRequest(t, "POST", "/api/traps", integrationJSON(t, requests[i]), admin)
		wg.Go(func() {
			<-gate
			responses[i] = serveOperator(r.Router, req)
		})
	}
	close(gate)
	wg.Wait()
	winner, loser := 0, 1
	if responses[1].Code == 201 {
		winner, loser = 1, 0
	}
	if responses[winner].Code != 201 || responses[loser].Code != 409 {
		t.Fatalf("concurrent port allocation: %d %s; %d %s", responses[0].Code, responses[0].Body.String(), responses[1].Code, responses[1].Body.String())
	}
	requireOperatorError(t, responses[loser], "trap_port_conflict")
	trap := decodeIntegration[traps.Trap](t, responses[winner])
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, requests[winner]), admins[winner], 200)

	// A rejected request reserves neither the ID nor the port. Deletion releases it.
	trapMutation(t, r, admins[winner], "DELETE", "/api/traps/"+trap.ID, "", trap.Revision, 204)
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, requests[loser]), admins[loser], 201)

	different := catalogProfileRequest(t, r, admins[winner])
	different.Config["listeners"].([]any)[0].(profiles.Object)["port"] = 2223
	p := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, different), admins[winner], 201))
	sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Different port", ProfileID: p.ID}), admins[winner], 201)
}

func TestManagedTrapApplyConfigRejectsOccupiedPortWithoutSideEffects(t *testing.T) {
	r := openIntegrationRuntime(t)
	r.Traps.WithAutoDeploy(true)
	admin := registerOperator(t, r, "apply-ports@example.test", auth.OrganizationInput{Mode: "create", Name: "Ports"})
	var target traps.Trap
	var profile profiles.Profile
	var other traps.Trap
	var otherProfile profiles.Profile
	for i, port := range []int{2222, 2223} {
		req := catalogProfileRequest(t, r, admin)
		req.Config["listeners"].([]any)[0].(profiles.Object)["port"] = port
		p := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, req), admin, 201))
		created := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Apply port", ProfileID: p.ID}), admin, 201))
		if i == 0 {
			target, profile = created, p
		} else {
			other, otherProfile = created, p
		}
	}
	path := "/api/traps/" + target.ID
	config := profile.Config
	config["listeners"].([]any)[0].(map[string]any)["port"] = 2223
	profile = decodeIntegration[profiles.Profile](t, sendOperator(t, r, "PATCH", "/api/profiles/"+profile.ID, integrationJSON(t, profiles.Object{"config": config}), admin, 200, profiles.ETag(profile)))
	request := commands.CreateRequest{RequestID: string(contract.NewID()), Action: "apply_config", Params: json.RawMessage(integrationJSON(t, profiles.Object{"profile_revision": profile.Revision}))}
	requireOperatorError(t, sendOperator(t, r, "POST", path+"/commands", integrationJSON(t, request), admin, 409), "trap_port_conflict")
	current := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", admin, 200))
	if current.StateVersion != target.StateVersion || current.ActiveCommandID != nil {
		t.Fatalf("rejected configuration changed trap: %+v", current)
	}
	var count int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM commands WHERE trap_id=$1`, target.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected configuration created command: count=%d err=%v", count, err)
	}
	config["listeners"].([]any)[0].(map[string]any)["port"] = 2222
	profile = decodeIntegration[profiles.Profile](t, sendOperator(t, r, "PATCH", "/api/profiles/"+profile.ID, integrationJSON(t, profiles.Object{"config": config}), admin, 200, profiles.ETag(profile)))
	request.Params = json.RawMessage(integrationJSON(t, profiles.Object{"profile_revision": profile.Revision}))
	sendOperator(t, r, "POST", path+"/commands", integrationJSON(t, request), admin, 201)
	// Editing a profile does not release a port pinned by an already queued command.
	config["listeners"].([]any)[0].(map[string]any)["port"] = 2224
	sendOperator(t, r, "PATCH", "/api/profiles/"+profile.ID, integrationJSON(t, profiles.Object{"config": config}), admin, 200, profiles.ETag(profile))
	otherProfile.Config["listeners"].([]any)[0].(map[string]any)["port"] = 2222
	otherProfile = decodeIntegration[profiles.Profile](t, sendOperator(t, r, "PATCH", "/api/profiles/"+otherProfile.ID, integrationJSON(t, profiles.Object{"config": otherProfile.Config}), admin, 200, profiles.ETag(otherProfile)))
	request.RequestID = string(contract.NewID())
	request.Params = json.RawMessage(integrationJSON(t, profiles.Object{"profile_revision": otherProfile.Revision}))
	join := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, r, "apply-ports-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: join.Code})
	sendOperator(t, r, "POST", "/api/traps/"+other.ID+"/commands", integrationJSON(t, request), viewer, 403)
	requireOperatorError(t, sendOperator(t, r, "POST", "/api/traps/"+other.ID+"/commands", integrationJSON(t, request), admin, 409), "trap_port_conflict")
}
