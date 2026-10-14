package http

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"honey-forge/modules/auth"
	"honey-forge/modules/auth/internal/teststore"
	"honey-forge/modules/auth/service"
)

func TestAdminPanelPasswordHTTPBoundary(t *testing.T) {
	repo := teststore.New()
	s := service.NewService(repo)
	owner, raw, err := s.Register(t.Context(), createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	router := gin.New()
	h := NewHandler(s, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err := h.RegisterRoutes(router); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path, body, cookie, csrf string
		status                                 int
		code                                   string
	}{
		{"locked members", "GET", "/api/organization/members", "", raw, "", 403, "admin_confirmation_required"},
		{"locked permission", "PATCH", "/api/organization/members/" + owner.User.ID + "/trap-permission", `{}`, raw, owner.CSRFToken, 403, "admin_confirmation_required"},
		{"anonymous", "POST", "/api/admin-session", `{"password":"correct horse battery staple"}`, "", "", 401, "unauthenticated"},
		{"missing csrf", "POST", "/api/admin-session", `{"password":"correct horse battery staple"}`, raw, "", 403, "csrf_failed"},
		{"malformed", "POST", "/api/admin-session", `{`, raw, owner.CSRFToken, 400, "invalid_json"},
		{"extra identity", "POST", "/api/admin-session", `{"password":"correct horse battery staple","user_id":"other"}`, raw, owner.CSRFToken, 400, "invalid_json"},
		{"missing password", "POST", "/api/admin-session", `{}`, raw, owner.CSRFToken, 422, "validation_failed"},
		{"wrong password", "POST", "/api/admin-session", `{"password":"incorrect-password"}`, raw, owner.CSRFToken, 401, "invalid_credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := authRequest(router, tc.method, tc.path, tc.body, tc.cookie, tc.csrf)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("response=%d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("admin access response can be cached")
			}
		})
	}
	request := httptest.NewRequest("POST", "/api/admin-session", strings.NewReader(`{"password":"correct horse battery staple"}`))
	request.Header.Set("Authorization", "Bearer secret")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: raw})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("bearer confirmed admin access")
	}
	response = authRequest(router, "POST", "/api/admin-session", `{"password":"correct horse battery staple"}`, raw, owner.CSRFToken)
	var view auth.AdminAccessView
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &view) != nil || view.ExpiresAt == nil {
		t.Fatalf("unlock=%d %s", response.Code, response.Body.String())
	}
	if response := authRequest(router, "GET", "/api/organization/members", "", raw, ""); response.Code != 200 {
		t.Fatal("confirmation did not unlock panel")
	}
	if response := authRequest(router, "DELETE", "/api/admin-session", "", raw, ""); response.Code != 403 {
		t.Fatal("lock without CSRF accepted")
	}
	if response := authRequest(router, "DELETE", "/api/admin-session", "", raw, owner.CSRFToken); response.Code != 204 {
		t.Fatal("lock failed")
	}
	if response := authRequest(router, "GET", "/api/organization/members", "", raw, ""); response.Code != 403 {
		t.Fatal("panel stayed unlocked")
	}
	if strings.Contains(logs.String(), "correct horse battery staple") || strings.Contains(logs.String(), "incorrect-password") {
		t.Fatal("password in logs")
	}
	for range 3 {
		authRequest(router, "POST", "/api/admin-session", `{"password":"incorrect-password"}`, raw, owner.CSRFToken)
	}
	response = authRequest(router, "POST", "/api/admin-session", `{"password":"correct horse battery staple"}`, raw, owner.CSRFToken)
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatalf("quota=%d %s", response.Code, response.Body.String())
	}
}
