//go:build integration

package app

import (
	"testing"

	"honey-forge/modules/auth"
)

func TestMemberRemovalProtectsOrganizationAccess(t *testing.T) {
	r := openIntegrationRuntime(t)
	owner := registerOperator(t, r, "owner@example.test", auth.OrganizationInput{Mode: "create", Name: "Team"})
	code := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", owner, 200))
	viewer := registerOperator(t, r, "viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: code.Code})
	foreign := registerOperator(t, r, "foreign@example.test", auth.OrganizationInput{Mode: "create", Name: "Other team"})
	_, trap, _ := createIntegrationTrap(t, r, owner)
	path := "/api/organization/members/" + viewer.view.User.ID
	sendOperator(t, r, "POST", "/api/admin-session", `{"password":"demo-password-2026"}`, owner, 200)
	for _, tc := range []struct{ name, origin, csrf string }{
		{"foreign origin", "https://attacker.example", owner.view.CSRFToken},
		{"missing origin", "", owner.view.CSRFToken},
		{"missing csrf", integrationOrigin, ""},
		{"wrong csrf", integrationOrigin, viewer.view.CSRFToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := operatorRequest(t, "DELETE", path, "", owner)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-CSRF-Token", tc.csrf)
			if w := serveOperator(r.Router, req); w.Code != 403 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	sendOperator(t, r, "DELETE", path, "", viewer, 403)
	sendOperator(t, r, "DELETE", "/api/organization/members/"+foreign.view.User.ID, "", owner, 404)
	sendOperator(t, r, "GET", "/api/traps/"+trap.ID, "", viewer, 200)
	sendOperator(t, r, "DELETE", path, "", owner, 204)
	sendOperator(t, r, "GET", "/api/session", "", viewer, 401)
	sendOperator(t, r, "GET", "/api/traps/"+trap.ID, "", viewer, 401)
	sendOperator(t, r, "GET", "/api/session", "", foreign, 200)
	sendOperator(t, r, "GET", "/api/traps/"+trap.ID, "", owner, 200)
	page := decodeIntegration[auth.MemberPage](t, sendOperator(t, r, "GET", "/api/organization/members", "", owner, 200))
	if len(page.Items) != 1 || page.Items[0].ID != owner.view.User.ID {
		t.Fatalf("unexpected members: %+v", page)
	}
}
