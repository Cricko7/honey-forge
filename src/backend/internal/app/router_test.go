package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Cricko7/honey-forge/src/backend/modules/auth"
)

func testRouter(t *testing.T, output io.Writer) http.Handler {
	t.Helper()

	service := auth.NewService(nil)
	logger := slog.New(slog.NewJSONHandler(output, nil))
	router, err := NewRouter(service, logger, "https://example.com")
	if err != nil {
		t.Fatal(err)
	}

	return router
}

func TestRouterErrorsAndCSRFProtection(t *testing.T) {
	router := testRouter(t, io.Discard)

	tests := []struct {
		name   string
		method string
		path   string
		origin string
		status int
		code   string
	}{
		{name: "unknown route", method: "GET", path: "/missing", status: 404, code: "resource_not_found"},
		{name: "unversioned auth route", method: "GET", path: "/api/auth/me", status: 404, code: "resource_not_found"},
		{name: "trailing slash", method: "GET", path: "/healthz/", status: 404, code: "resource_not_found"},
		{name: "wrong method", method: "GET", path: "/api/registrations", status: 405, code: "method_not_allowed"},
		{name: "missing cookie", method: "GET", path: "/api/session", status: 401, code: "unauthenticated"},
		{name: "cross origin logout", method: "DELETE", path: "/api/session", origin: "https://attacker.example", status: 403, code: "origin_not_allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, "http://example.com"+tt.path, nil)
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}

			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.status, response.Body.String())
			}

			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}

			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}

			if envelope.Error.Code != tt.code || envelope.Error.Message == "" {
				t.Fatalf("incorrect error response: %s", response.Body.String())
			}

			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("every response must have a request ID")
			}

			if tt.status == http.StatusMethodNotAllowed && response.Header().Get("Allow") == "" {
				t.Fatal("a 405 response must name the allowed methods")
			}
		})
	}
}

func TestAuthRateLimitIgnoresSpoofedProxyHeaders(t *testing.T) {
	router := testRouter(t, io.Discard)

	for attempt := range 11 {
		request := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("X-Forwarded-For", strings.Repeat("1", attempt+1))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		if attempt < 10 && response.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("unexpected status before limit: %d", response.Code)
		}

		if attempt == 10 && (response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "") {
			t.Fatal("spoofed proxy headers must not bypass the auth rate limit")
		}
	}

	response := httptest.NewRecorder()
	logout := httptest.NewRequest(http.MethodDelete, "/api/session", nil)
	logout.Header.Set("Origin", "https://example.com")
	router.ServeHTTP(response, logout)

	if response.Code != http.StatusNoContent {
		t.Fatal("rate limits must not prevent logout")
	}
}

func TestRequestLogsExcludeSecrets(t *testing.T) {
	var output bytes.Buffer
	router := testRouter(t, &output)

	request := httptest.NewRequest(http.MethodPost, "/missing?token=query-secret", strings.NewReader("body-secret"))
	request.Header.Set("Authorization", "Bearer header-secret")
	request.AddCookie(&http.Cookie{Name: "refresh_token", Value: "cookie-secret"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	for _, secret := range []string{"query-secret", "body-secret", "header-secret", "cookie-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("request log exposed %s", secret)
		}
	}

	if !strings.Contains(output.String(), response.Header().Get("X-Request-ID")) {
		t.Fatal("log and response must use the same request ID")
	}
}
