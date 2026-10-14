//go:build integration

package app

import (
	"testing"

	"honey-forge/modules/auth"
)

func TestAdminPanelPasswordRequiresOriginAndIndependentConfirmation(t *testing.T) {
	r := openIntegrationRuntime(t)
	owner := registerOperator(t, r, "owner@example.test", auth.OrganizationInput{Mode: "create", Name: "Team"})
	sendOperator(t, r, "GET", "/api/organization/members", "", owner, 403)
	locked := decodeIntegration[auth.AdminAccessView](t, sendOperator(t, r, "GET", "/api/admin-session", "", owner, 200))
	if locked.ExpiresAt != nil {
		t.Fatal("registration implicitly confirmed admin password")
	}
	badOrigin := operatorRequest(t, "POST", "/api/admin-session", `{"password":"demo-password-2026"}`, owner)
	badOrigin.Header.Set("Origin", "https://attacker.example")
	if w := serveOperator(r.Router, badOrigin); w.Code != 403 {
		t.Fatalf("cross-origin confirmation=%d", w.Code)
	}
	badCSRF := operatorRequest(t, "POST", "/api/admin-session", `{"password":"demo-password-2026"}`, owner)
	badCSRF.Header.Del("X-CSRF-Token")
	if w := serveOperator(r.Router, badCSRF); w.Code != 403 {
		t.Fatal("confirmation without CSRF")
	}
	sendOperator(t, r, "POST", "/api/admin-session", `{"password":"incorrect-password"}`, owner, 401)
	sendOperator(t, r, "GET", "/api/session", "", owner, 200)
	sendOperator(t, r, "GET", "/api/organization/members", "", owner, 403)
	confirmed := decodeIntegration[auth.AdminAccessView](t, sendOperator(t, r, "POST", "/api/admin-session", `{"password":"demo-password-2026"}`, owner, 200))
	if confirmed.ExpiresAt == nil || !confirmed.ExpiresAt.Before(owner.view.ExpiresAt) {
		t.Fatal("invalid confirmation deadline")
	}
	sendOperator(t, r, "GET", "/api/organization/members", "", owner, 200)
	login := sendOperator(t, r, "POST", "/api/sessions", `{"email":"owner@example.test","password":"demo-password-2026"}`, operatorSession{}, 201)
	other := sessionFromResponse(t, login)
	sendOperator(t, r, "GET", "/api/organization/members", "", other, 403)
	sendOperator(t, r, "DELETE", "/api/admin-session", "", owner, 204)
	sendOperator(t, r, "GET", "/api/organization/members", "", owner, 403)
	sendOperator(t, r, "GET", "/api/session", "", owner, 200)
	sendOperator(t, r, "DELETE", "/api/session", "", owner, 204)
	sendOperator(t, r, "POST", "/api/admin-session", `{"password":"demo-password-2026"}`, owner, 401)
}
