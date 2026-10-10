//go:build integration

package app

import (
	"reflect"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
)

func TestRealSessionCatalogAndProfileFlow(t *testing.T) {
	runtime := openIntegrationRuntime(t)
	admin := registerOperator(t, runtime, "admin@example.com", auth.OrganizationInput{Mode: "create", Name: "Demo"})
	if admin.view.User.Role != auth.RoleAdmin || admin.view.User.OrganizationID != admin.view.Organization.ID {
		t.Fatal("registration identity does not match organization")
	}

	invite := decodeIntegration[auth.JoinCode](t, sendOperator(t, runtime, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, runtime, "viewer@example.com", auth.OrganizationInput{Mode: "join", JoinCode: invite.Code})
	if viewer.view.User.Role != auth.RoleViewer || viewer.view.Organization.ID != admin.view.Organization.ID {
		t.Fatal("joined viewer has wrong role or organization")
	}

	input := catalogProfileRequest(t, runtime, admin)
	body := integrationJSON(t, input)
	created := sendOperator(t, runtime, "POST", "/api/profiles", body, admin, 201)
	profile := decodeIntegration[profiles.Profile](t, created)
	path, etag := created.Header().Get("Location"), created.Header().Get("ETag")
	if path != "/api/profiles/"+profile.ID || etag != profiles.ETag(profile) || profile.Revision != 1 || profile.TypeID != input.TypeID || int64(profile.TypeVersion) != input.TypeVersion || profile.InteractionLevel != "low" {
		t.Fatalf("created profile differs from catalog/request: %s", created.Body.String())
	}
	read := decodeIntegration[profiles.Profile](t, sendOperator(t, runtime, "GET", path, "", viewer, 200))
	if !reflect.DeepEqual(read, profile) {
		t.Fatal("viewer does not see the created profile")
	}
	page := decodeIntegration[profiles.Page](t, sendOperator(t, runtime, "GET", "/api/profiles?type_id="+input.TypeID, "", viewer, 200))
	if len(page.Items) != 1 || page.Items[0].ID != profile.ID || page.NextCursor != nil {
		t.Fatal("created profile missing from filtered list")
	}

	replayed := sendOperator(t, runtime, "POST", "/api/profiles", body, admin, 200)
	if replayed.Header().Get("Idempotency-Replayed") != "true" || !reflect.DeepEqual(decodeIntegration[profiles.Profile](t, replayed), profile) {
		t.Fatal("creation replay changed the profile")
	}
	conflicting := input
	conflicting.Name = "different request"
	requireOperatorError(t, sendOperator(t, runtime, "POST", "/api/profiles", integrationJSON(t, conflicting), admin, 409), "idempotency_conflict")

	config := validIntegrationConfig()
	config["listeners"].([]any)[0].(profiles.Object)["banner"] = "updated banner"
	patch := integrationJSON(t, profiles.Object{"name": "Updated", "config": config})
	updated := sendOperator(t, runtime, "PATCH", path, patch, admin, 200, etag)
	current := decodeIntegration[profiles.Profile](t, updated)
	if current.Revision != 2 || current.Name != "Updated" || current.TypeID != profile.TypeID || current.TypeVersion != profile.TypeVersion || !profiles.EqualJSON(current.Config, config) {
		t.Fatalf("profile update was not persisted: %s", updated.Body.String())
	}
	currentETag := updated.Header().Get("ETag")
	if currentETag != profiles.ETag(current) || currentETag == etag {
		t.Fatal("profile ETag did not follow revision")
	}
	noOp := sendOperator(t, runtime, "PATCH", path, patch, admin, 200, currentETag)
	if noOp.Header().Get("ETag") != currentETag || !reflect.DeepEqual(decodeIntegration[profiles.Profile](t, noOp), current) {
		t.Fatal("no-op update changed the revision or timestamps")
	}
	requireOperatorError(t, sendOperator(t, runtime, "PATCH", path, `{"name":"stale"}`, admin, 412, etag), "revision_mismatch")
	requireOperatorError(t, sendOperator(t, runtime, "DELETE", path, "", admin, 412, etag), "revision_mismatch")

	historical := sendOperator(t, runtime, "POST", "/api/profiles", body, admin, 200)
	if historical.Header().Get("Idempotency-Replayed") != "true" || historical.Header().Get("ETag") != "" || !reflect.DeepEqual(decodeIntegration[profiles.Profile](t, historical), profile) {
		t.Fatal("replay must return original revision without a current ETag")
	}
	if got := decodeIntegration[profiles.Profile](t, sendOperator(t, runtime, "GET", path, "", viewer, 200)); !reflect.DeepEqual(got, current) {
		t.Fatal("replay or stale mutation changed current state")
	}

	deleted := sendOperator(t, runtime, "DELETE", path, "", admin, 204, currentETag)
	if deleted.Body.Len() != 0 {
		t.Fatal("DELETE 204 has a response body")
	}
	requireOperatorError(t, sendOperator(t, runtime, "GET", path, "", viewer, 404), "resource_not_found")
	requireOperatorError(t, sendOperator(t, runtime, "POST", "/api/profiles", body, admin, 409), "request_already_used")
	empty := decodeIntegration[profiles.Page](t, sendOperator(t, runtime, "GET", "/api/profiles", "", viewer, 200))
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatal("deleted profile remains in list or empty list violates contract")
	}
}

func TestRealSessionRevocationProtectsCatalogAndProfiles(t *testing.T) {
	runtime := openIntegrationRuntime(t)
	admin := registerOperator(t, runtime, "admin@example.com", auth.OrganizationInput{Mode: "create", Name: "Demo"})
	input := catalogProfileRequest(t, runtime, admin)
	created := sendOperator(t, runtime, "POST", "/api/profiles", integrationJSON(t, input), admin, 201)
	path := created.Header().Get("Location")

	sendOperator(t, runtime, "DELETE", "/api/session", "", admin, 204)
	for _, tc := range []struct{ name, path string }{
		{"session", "/api/session"},
		{"catalog", "/api/trap-types"},
		{"catalog_version", "/api/trap-types/tcp-banner/versions/1"},
		{"profiles", "/api/profiles"},
		{"profile", path},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := operatorRequest(t, "GET", tc.path, "", admin)
			req.Header.Set("If-None-Match", "*")
			w := serveOperator(runtime.Router, req)
			if w.Code != 401 {
				t.Fatalf("revoked session: status %d, want 401: %s", w.Code, w.Body.String())
			}
			requireOperatorError(t, w, "unauthenticated")
		})
	}
	requireOperatorError(t, sendOperator(t, runtime, "POST", "/api/profiles", integrationJSON(t, input), admin, 401), "unauthenticated")

	login := sendOperator(t, runtime, "POST", "/api/sessions", integrationJSON(t, auth.LoginRequest{Email: admin.view.User.Email, Password: integrationPassword}), operatorSession{}, 201)
	fresh := sessionFromResponse(t, login)
	if fresh.cookie == admin.cookie || fresh.view.CSRFToken == admin.view.CSRFToken || fresh.view.User.ID != admin.view.User.ID {
		t.Fatal("login did not create a fresh session for the same user")
	}
	read := sendOperator(t, runtime, "GET", path, "", fresh, 200)
	if decodeIntegration[profiles.Profile](t, read).ID != decodeIntegration[profiles.Profile](t, created).ID {
		t.Fatal("profile disappeared after logout/login")
	}
	catalogProfileRequest(t, runtime, fresh)

	wrongCSRF := fresh
	wrongCSRF.view.CSRFToken = admin.view.CSRFToken
	requireOperatorError(t, sendOperator(t, runtime, "PATCH", path, `{"name":"wrong session"}`, wrongCSRF, 403, created.Header().Get("ETag")), "csrf_failed")
}
