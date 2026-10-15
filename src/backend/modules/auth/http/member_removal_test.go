package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/auth/internal/teststore"
	"honey-forge/modules/auth/service"
)

func TestRemoveMemberHTTP(t *testing.T) {
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
	join := createRequest("viewer@example.com")
	join.Organization = &auth.OrganizationInput{Mode: "join", JoinCode: code.Code}
	viewer, viewerRaw, err := s.Register(t.Context(), join, "")
	if err != nil {
		t.Fatal(err)
	}
	foreign, _, err := s.Register(t.Context(), createRequest("foreign@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	router := authRouter(t, repo)
	path := "/api/organization/members/" + viewer.User.ID
	if w := authRequest(router, "DELETE", path, "", raw, owner.CSRFToken); w.Code != 403 {
		t.Fatalf("unconfirmed: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), sess, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, body, cookie, csrf string
		status                         int
	}{
		{"anonymous", path, "", "", "", 401},
		{"csrf required", path, "", raw, "", 403},
		{"viewer forbidden", path, "", viewerRaw, viewer.CSRFToken, 403},
		{"foreign target", "/api/organization/members/" + foreign.User.ID, "", raw, owner.CSRFToken, 404},
		{"creator protected", "/api/organization/members/" + owner.User.ID, "", raw, owner.CSRFToken, 409},
		{"invalid id", "/api/organization/members/invalid", "", raw, owner.CSRFToken, 400},
		{"unexpected query", path + "?organization_id=" + foreign.Organization.ID, "", raw, owner.CSRFToken, 400},
		{"unexpected body", path, `{"force":true}`, raw, owner.CSRFToken, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := authRequest(router, "DELETE", tc.path, tc.body, tc.cookie, tc.csrf)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("response can be cached")
			}
		})
	}
	req := httptest.NewRequest("DELETE", path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: raw})
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("bearer accepted: %d", w.Code)
	}
	if w := authRequest(router, "DELETE", path, "", raw, owner.CSRFToken); w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	if w := authRequest(router, "GET", "/api/session", "", viewerRaw, ""); w.Code != 401 {
		t.Fatalf("removed member authenticated: %d", w.Code)
	}
	if w := authRequest(router, "DELETE", path, "", raw, owner.CSRFToken); w.Code != 404 {
		t.Fatalf("repeated removal: %d", w.Code)
	}
}
