//go:build integration

package repository

import (
	authcore "honey-forge/src/backend/modules/auth"
	"net/http"
	"net/http/httptest"
	"strings"
)

func createRequest(email string) authcore.RegisterRequest {
	return authcore.RegisterRequest{Email: email, Password: "demo-password-2026", Organization: &authcore.OrganizationInput{Mode: "create", Name: "Demo"}}
}
func authRequest(router http.Handler, method, path, body, cookie, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-session", Value: cookie})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}
