//go:build integration

package app

import (
	"honey-forge/internal/contract"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/traps"
)

func TestDelegationControlsRealTrapRequests(t *testing.T) {
	r := openIntegrationRuntime(t)
	owner := registerOperator(t, r, "owner@example.test", auth.OrganizationInput{Mode: "create", Name: "Team"})
	code := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", owner, 200))
	viewer := registerOperator(t, r, "viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: code.Code})
	profile, trap, _ := createIntegrationTrap(t, r, owner)
	path := "/api/traps/" + trap.ID
	grantPath := "/api/organization/members/" + viewer.view.User.ID + "/trap-permission"
	trapMutation(t, r, viewer, "PATCH", path, `{"name":"Unauthorized"}`, 1, 403)
	badOrigin := operatorRequest(t, "PATCH", grantPath, `{"can_manage_traps":true,"expected_revision":1}`, owner)
	badOrigin.Header.Set("Origin", "https://attacker.example")
	if w := serveOperator(r.Router, badOrigin); w.Code != 403 {
		t.Fatalf("cross origin=%d", w.Code)
	}
	sendOperator(t, r, "PATCH", grantPath, `{"can_manage_traps":true,"expected_revision":1}`, owner, 204)
	trapMutation(t, r, viewer, "PATCH", path, `{"name":"Delegated change"}`, 1, 200)
	created := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", `{"request_id":"`+string(contract.NewID())+`","name":"Delegate trap","profile_id":"`+profile.ID+`"}`, viewer, 201))
	trapMutation(t, r, viewer, "DELETE", "/api/traps/"+created.ID, "", 1, 204)
	sendOperator(t, r, "POST", "/api/profiles", `{}`, viewer, 403)
	for _, private := range []string{"/api/organization/members", "/api/api-keys", "/api/organization/join-code", path + "/agent-credentials"} {
		sendOperator(t, r, "GET", private, "", viewer, 403)
	}
	// Command authorization allows the delegate through to shape validation.
	sendOperator(t, r, "POST", path+"/commands", `{}`, viewer, 422)
	connectIntegrationAgent(t, r, owner, trap)
	sendOperator(t, r, "POST", path+"/commands", `{"request_id":"`+string(contract.NewID())+`","action":"stop","params":{}}`, viewer, 201)
	sendOperator(t, r, "PATCH", grantPath, `{"can_manage_traps":false,"expected_revision":2}`, owner, 204)
	trapMutation(t, r, viewer, "PATCH", path, `{"name":"Revoked change"}`, 2, 403)
	sendOperator(t, r, "POST", path+"/commands", `{}`, viewer, 403)
	read := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", viewer, 200))
	if read.Name != "Delegated change" {
		t.Fatal("revoked request changed trap")
	}
}
