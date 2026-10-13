package http

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	authcore "honey-forge/modules/auth"
	"honey-forge/modules/auth/internal/teststore"
)

func TestAPIKeysLifecycle(t *testing.T) {
	router := authRouter(t, teststore.New())
	registered := authRequest(router, "POST", "/api/registrations", `{"email":"keys@example.com","password":"demo-password-2026","organization":{"mode":"create","name":"Keys"}}`, "", "")
	var session authcore.SessionView
	if err := json.Unmarshal(registered.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	cookie := registered.Result().Cookies()[0].Value
	created := authRequest(router, "POST", "/api/api-keys", `{"name":"Integration"}`, cookie, session.CSRFToken)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}

	var key struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &key); err != nil {
		t.Fatal(err)
	}

	if key.ID == "" || !strings.HasPrefix(key.Token, "hf_") {
		t.Fatal("missing key")
	}

	list := authRequest(router, "GET", "/api/api-keys", "", cookie, "")
	if list.Code != 200 || strings.Contains(list.Body.String(), key.Token) {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}

	bearer := func(token string) int {
		req := httptest.NewRequest("GET", "/api/organization", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		return w.Code
	}
	if status := bearer(key.Token); status != 200 {
		t.Fatalf("bearer status: %d", status)
	}

	revoked := authRequest(router, "DELETE", "/api/api-keys/"+key.ID, "", cookie, session.CSRFToken)
	if revoked.Code != 204 {
		t.Fatalf("revoke: %d %s", revoked.Code, revoked.Body.String())
	}

	if status := bearer(key.Token); status != 401 {
		t.Fatalf("revoked status: %d", status)
	}
}

func TestAPIKeyInvalidRequests(t *testing.T) {
	router := authRouter(t, teststore.New())
	registered := authRequest(router, "POST", "/api/registrations", `{"email":"invalidkeys@example.com","password":"demo-password-2026","organization":{"mode":"create","name":"Keys"}}`, "", "")
	var session authcore.SessionView
	if err := json.Unmarshal(registered.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	cookie := registered.Result().Cookies()[0].Value
	for _, tc := range []struct {
		name, body, csrf string
		status           int
	}{
		{"empty name", `{"name":""}`, session.CSRFToken, 422},
		{"malformed", `{`, session.CSRFToken, 400},
		{"unknown field", `{"name":"Key","token":"secret"}`, session.CSRFToken, 400},
		{"CSRF", `{"name":"Key"}`, "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := authRequest(router, "POST", "/api/api-keys", tc.body, cookie, tc.csrf)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}

	req := httptest.NewRequest("GET", "/api/organization", nil)
	req.AddCookie(registered.Result().Cookies()[0])
	req.Header.Set("Authorization", "Bearer invalid")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("invalid bearer fell back to cookie: %d", w.Code)
	}
}
