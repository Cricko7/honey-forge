//go:build integration

package app

import (
	"net/url"
	"reflect"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
)

func TestRealSessionsEnforceProfilePermissions(t *testing.T) {
	runtime := openIntegrationRuntime(t)
	admin := registerOperator(t, runtime, "admin@example.com", auth.OrganizationInput{Mode: "create", Name: "Demo"})
	invite := decodeIntegration[auth.JoinCode](t, sendOperator(t, runtime, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, runtime, "viewer@example.com", auth.OrganizationInput{Mode: "join", JoinCode: invite.Code})
	input := catalogProfileRequest(t, runtime, viewer)
	body := integrationJSON(t, input)
	created := sendOperator(t, runtime, "POST", "/api/profiles", body, admin, 201)
	path, etag := created.Header().Get("Location"), created.Header().Get("ETag")
	original := decodeIntegration[profiles.Profile](t, created)

	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"create", "POST", "/api/profiles", body},
		{"patch", "PATCH", path, `{"name":"forbidden"}`},
		{"delete", "DELETE", path, ""},
	} {
		t.Run("viewer/"+tc.name, func(t *testing.T) {
			requireOperatorError(t, sendOperator(t, runtime, tc.method, tc.path, tc.body, viewer, 403, etag), "forbidden")
		})
	}

	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"catalog", "GET", "/api/trap-types", ""},
		{"catalog_version", "GET", "/api/trap-types/tcp-banner/versions/1", ""},
		{"list", "GET", "/api/profiles", ""},
		{"read", "GET", path, ""},
		{"create", "POST", "/api/profiles", body},
		{"patch", "PATCH", path, `{"name":"forbidden"}`},
		{"delete", "DELETE", path, ""},
	} {
		t.Run("anonymous/"+tc.name, func(t *testing.T) {
			requireOperatorError(t, sendOperator(t, runtime, tc.method, tc.path, tc.body, operatorSession{}, 401, etag), "unauthenticated")
		})
	}

	for _, tc := range []struct {
		name, origin, csrf, code string
	}{
		{"missing_csrf", integrationOrigin, "", "csrf_failed"},
		{"wrong_csrf", integrationOrigin, viewer.view.CSRFToken, "csrf_failed"},
		{"missing_origin", "", admin.view.CSRFToken, "origin_not_allowed"},
		{"foreign_origin", "https://foreign.example", admin.view.CSRFToken, "origin_not_allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := operatorRequest(t, "PATCH", path, `{"name":"forbidden"}`, admin)
			req.Header.Set("If-Match", etag)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-CSRF-Token", tc.csrf)
			w := serveOperator(runtime.Router, req)
			if w.Code != 403 {
				t.Fatalf("status %d, want 403: %s", w.Code, w.Body.String())
			}
			requireOperatorError(t, w, tc.code)
		})
	}

	read := decodeIntegration[profiles.Profile](t, sendOperator(t, runtime, "GET", path, "", viewer, 200))
	page := decodeIntegration[profiles.Page](t, sendOperator(t, runtime, "GET", "/api/profiles", "", viewer, 200))
	if !reflect.DeepEqual(read, original) || len(page.Items) != 1 {
		t.Fatal("rejected requests changed or created profiles")
	}
}

func TestRealOrganizationsIsolateProfilesAndCursors(t *testing.T) {
	runtime := openIntegrationRuntime(t)
	owner := registerOperator(t, runtime, "owner@example.com", auth.OrganizationInput{Mode: "create", Name: "Owner"})
	foreign := registerOperator(t, runtime, "foreign@example.com", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	input := catalogProfileRequest(t, runtime, owner)
	body := integrationJSON(t, input)
	created := sendOperator(t, runtime, "POST", "/api/profiles", body, owner, 201)
	path, etag := created.Header().Get("Location"), created.Header().Get("ETag")
	original := decodeIntegration[profiles.Profile](t, created)

	for _, tc := range []struct {
		method, body string
	}{
		{"GET", ""},
		{"PATCH", `{"name":"foreign"}`},
		{"DELETE", ""},
	} {
		t.Run(tc.method, func(t *testing.T) {
			requireOperatorError(t, sendOperator(t, runtime, tc.method, path, tc.body, foreign, 404, etag), "resource_not_found")
		})
	}
	empty := decodeIntegration[profiles.Page](t, sendOperator(t, runtime, "GET", "/api/profiles", "", foreign, 200))
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatal("foreign organization can list owner's profiles")
	}

	// Idempotency belongs to the authenticated organization, even for identical bodies.
	other := decodeIntegration[profiles.Profile](t, sendOperator(t, runtime, "POST", "/api/profiles", body, foreign, 201))
	if other.ID == original.ID {
		t.Fatal("request_id was shared across organizations")
	}
	second := input
	second.RequestID = profiles.NewID()
	second.Name = "Second"
	sendOperator(t, runtime, "POST", "/api/profiles", integrationJSON(t, second), owner, 201)
	firstPage := decodeIntegration[profiles.Page](t, sendOperator(t, runtime, "GET", "/api/profiles?limit=1&type_id=tcp-banner", "", owner, 200))
	if len(firstPage.Items) != 1 || firstPage.NextCursor == nil {
		t.Fatal("two profiles did not produce pagination cursor")
	}
	cursorPath := "/api/profiles?limit=1&type_id=tcp-banner&cursor=" + url.QueryEscape(*firstPage.NextCursor)
	requireOperatorError(t, sendOperator(t, runtime, "GET", cursorPath, "", foreign, 400), "invalid_cursor")

	newer := input
	newer.RequestID = profiles.NewID()
	newer.Name = "After first page"
	newProfile := decodeIntegration[profiles.Profile](t, sendOperator(t, runtime, "POST", "/api/profiles", integrationJSON(t, newer), owner, 201))
	nextPage := decodeIntegration[profiles.Page](t, sendOperator(t, runtime, "GET", cursorPath, "", owner, 200))
	if len(nextPage.Items) != 1 || nextPage.Items[0].ID == firstPage.Items[0].ID || nextPage.Items[0].ID == newProfile.ID || nextPage.Items[0].ID == other.ID || nextPage.NextCursor != nil {
		t.Fatal("pagination repeated, leaked, or included a profile created after its boundary")
	}
	current := decodeIntegration[profiles.Profile](t, sendOperator(t, runtime, "GET", path, "", owner, 200))
	if !reflect.DeepEqual(current, original) {
		t.Fatal("foreign organization changed owner's profile")
	}
}
