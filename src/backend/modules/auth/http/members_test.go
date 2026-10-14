package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/auth/internal/teststore"
	"honey-forge/modules/auth/service"
)

func TestMembersHTTPBoundaryAndPresence(t *testing.T) {
	repo := teststore.New()
	s := service.NewService(repo)
	owner, raw, err := s.Register(t.Context(), createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.ResolveSession(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.JoinCode(t.Context(), sess.AuthContext())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), sess, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	join := createRequest("viewer@example.com")
	join.Organization = &auth.OrganizationInput{Mode: "join", JoinCode: code.Code}
	viewer, viewerRaw, err := s.Register(t.Context(), join, "")
	if err != nil {
		t.Fatal(err)
	}
	router := authRouter(t, repo)
	path := "/api/organization/members/" + viewer.User.ID + "/trap-permission"
	for _, tc := range []struct {
		name, method, path, body, cookie, csrf string
		status                                 int
	}{
		{"anonymous", "GET", "/api/organization/members", "", "", "", 401},
		{"viewer list", "GET", "/api/organization/members", "", viewerRaw, "", 403},
		{"foreign org query", "GET", "/api/organization/members?organization_id=" + owner.Organization.ID, "", raw, "", 400},
		{"duplicate query", "GET", "/api/organization/members?limit=1&limit=2", "", raw, "", 400},
		{"invalid limit", "GET", "/api/organization/members?limit=101", "", raw, "", 400},
		{"invalid cursor", "GET", "/api/organization/members?cursor=oops", "", raw, "", 400},
		{"missing csrf", "PATCH", path, `{"can_manage_traps":true,"expected_revision":1}`, raw, "", 403},
		{"self grant", "PATCH", path, `{"can_manage_traps":true,"expected_revision":1}`, viewerRaw, viewer.CSRFToken, 403},
		{"mass assignment", "PATCH", path, `{"can_manage_traps":true,"expected_revision":1,"role":"admin"}`, raw, owner.CSRFToken, 400},
		{"missing permission", "PATCH", path, `{"expected_revision":1}`, raw, owner.CSRFToken, 422},
		{"missing revision", "PATCH", path, `{"can_manage_traps":true}`, raw, owner.CSRFToken, 422},
		{"malformed", "PATCH", path, `{`, raw, owner.CSRFToken, 400},
		{"presence csrf", "POST", "/api/session/presence", "", viewerRaw, "", 403},
		{"presence cannot select user", "POST", "/api/session/presence", `{"user_id":"other"}`, viewerRaw, viewer.CSRFToken, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := authRequest(router, tc.method, tc.path, tc.body, tc.cookie, tc.csrf)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("administrative response can be cached")
			}
		})
	}
	request := httptest.NewRequest("GET", "/api/organization/members", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: raw})
	request.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("bearer read administration")
	}
	if w := authRequest(router, "PATCH", path, `{"can_manage_traps":true,"expected_revision":1}`, raw, owner.CSRFToken); w.Code != 204 {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	if w := authRequest(router, "POST", "/api/session/presence", "", viewerRaw, viewer.CSRFToken); w.Code != 204 {
		t.Fatalf("presence: %d %s", w.Code, w.Body.String())
	}
	list := authRequest(router, "GET", "/api/organization/members", "", raw, "")
	var page auth.MemberPage
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if list.Code != 200 || page.Creator == nil || page.Creator.ID != owner.User.ID || len(page.Items) != 2 {
		t.Fatalf("list=%s", list.Body.String())
	}
	for _, member := range page.Items {
		if member.ID == viewer.User.ID && (!member.Online || !member.CanManageTraps || member.PermissionRevision != 2) {
			t.Fatal("incorrect member state")
		}
	}
	for _, secret := range []string{raw, viewerRaw, code.Code, owner.CSRFToken, "password_hash", "token_hash", "csrf_hash"} {
		if strings.Contains(list.Body.String(), secret) {
			t.Fatal("secret exposed in member list")
		}
	}
	first := authRequest(router, "GET", "/api/organization/members?limit=1", "", raw, "")
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("pagination not bounded")
	}
	second := authRequest(router, "GET", "/api/organization/members?limit=1&cursor="+*page.NextCursor, "", raw, "")
	var next auth.MemberPage
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.NextCursor != nil {
		t.Fatal("pagination skips or repeats")
	}
	if err := s.Logout(t.Context(), viewerRaw, viewer.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if w := authRequest(router, "POST", "/api/session/presence", "", viewerRaw, viewer.CSRFToken); w.Code != 401 {
		t.Fatal("revoked session sent presence")
	}
}
