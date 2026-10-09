package http

import (
	"bytes"
	"encoding/json"
	"errors"
	authcore "honey-forge/src/backend/modules/auth"
	authservice "honey-forge/src/backend/modules/auth/service"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

func authRouter(t *testing.T, repo *memoryStore) http.Handler {
	t.Helper()

	router := gin.New()
	handler := NewHandler(authservice.NewService(repo), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err := handler.RegisterRoutes(router); err != nil {
		t.Fatal(err)
	}

	return router
}

func authRequest(router http.Handler, method, path, body, cookie, csrf string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func TestHTTPRegistrationSessionOrganizationAndLogout(t *testing.T) {
	repo := newMemoryStore()
	router := authRouter(t, repo)
	response := authRequest(router, "POST", "/api/registrations",
		`{"email":" Admin@Example.com ","password":"demo-password-2026","organization":{"mode":"create","name":" Demo SOC "}}`, "", "")

	if response.Code != 201 || response.Header().Get("Location") != "/api/session" {
		t.Fatalf("registration: %d %s", response.Code, response.Body.String())
	}

	var view authcore.SessionView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-session" || cookie.Path != "/" || cookie.Domain != "" || !cookie.Secure ||
		!cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 604800 {
		t.Fatalf("invalid cookie: %+v", cookie)
	}
	if strings.Contains(response.Body.String(), cookie.Value) || strings.Contains(response.Body.String(), "demo-password-2026") {
		t.Fatal("response exposed password/cookie")
	}
	if view.User.Role != authcore.RoleAdmin || view.CSRFToken == "" {
		t.Fatal("missing admin/CSRF")
	}

	already := authRequest(router, "POST", "/api/registrations", "{}", cookie.Value, "")
	if already.Code != 409 || !strings.Contains(already.Body.String(), "already_authenticated") {
		t.Fatalf("authenticated registration must be rejected before binding: %d %s", already.Code, already.Body.String())
	}

	response = authRequest(router, "GET", "/api/session", "", cookie.Value, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var reload authcore.SessionView
	if err := json.Unmarshal(response.Body.Bytes(), &reload); err != nil {
		t.Fatal(err)
	}
	if reload.CSRFToken != view.CSRFToken || !reload.ExpiresAt.Equal(view.ExpiresAt) || len(response.Result().Cookies()) != 0 {
		t.Fatal("GET must recover CSRF without extending session")
	}

	response = authRequest(router, "GET", "/api/organization", "", cookie.Value, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "join_code") {
		t.Fatal("organization must omit join code")
	}

	response = authRequest(router, "POST", "/api/organization/join-code/rotations", `{"expected_revision":1}`, cookie.Value, "")
	if response.Code != 403 || !strings.Contains(response.Body.String(), "csrf_failed") {
		t.Fatal("rotation without CSRF accepted")
	}
	response = authRequest(router, "POST", "/api/organization/join-code/rotations", `{"expected_revision":1}`, cookie.Value, view.CSRFToken)
	if response.Code != 200 {
		t.Fatalf("rotation: %s", response.Body.String())
	}
	response = authRequest(router, "POST", "/api/organization/join-code/rotations", `{"expected_revision":1}`, cookie.Value, view.CSRFToken)
	if response.Code != 409 {
		t.Fatal("stale revision accepted")
	}

	response = authRequest(router, "DELETE", "/api/session", "", cookie.Value, "")
	if response.Code != 403 || len(response.Result().Cookies()) != 0 {
		t.Fatal("failed CSRF must preserve cookie/session")
	}
	response = authRequest(router, "DELETE", "/api/session", "", cookie.Value, view.CSRFToken)
	if response.Code != 204 || response.Body.Len() != 0 || response.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout must return empty 204 and delete cookie")
	}
	response = authRequest(router, "GET", "/api/session", "", cookie.Value, "")
	if response.Code != 401 {
		t.Fatal("revoked session authenticated")
	}
	response = authRequest(router, "DELETE", "/api/session", "", cookie.Value, "")
	if response.Code != 204 {
		t.Fatal("repeated logout must succeed")
	}
}

func TestHTTPViewerCannotReadOrRotateCode(t *testing.T) {
	repo := newMemoryStore()
	service := authservice.NewService(repo)
	_, adminRaw, err := service.Register(t.Context(), createRequest("admin@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := service.ResolveSession(t.Context(), adminRaw)
	code, _ := service.JoinCode(t.Context(), sess.AuthContext())
	req := createRequest("viewer@example.com")
	req.Organization = &authcore.OrganizationInput{Mode: "join", JoinCode: code.Code}
	view, raw, err := service.Register(t.Context(), req, "")
	if err != nil {
		t.Fatal(err)
	}

	router := authRouter(t, repo)
	if got := authRequest(router, "GET", "/api/organization/join-code", "", raw, ""); got.Code != 403 {
		t.Fatal("viewer read join code")
	}
	if got := authRequest(router, "POST", "/api/organization/join-code/rotations", `{"expected_revision":1}`, raw, view.CSRFToken); got.Code != 403 {
		t.Fatal("viewer rotated join code")
	}
}

func TestHTTPErrorsAndSensitiveDatabaseDetails(t *testing.T) {
	cases := []struct {
		err           error
		status        int
		code, message string
	}{
		{authcore.ErrEmailTaken, 409, "email_in_use", "Email is already registered"},
		{authcore.ErrAlreadyAuthenticated, 409, "already_authenticated", "Already authenticated"},
		{authcore.ErrInvalidCredentials, 401, "invalid_credentials", "Invalid email or password"},
		{authcore.ErrInvalidJoinCode, 422, "invalid_join_code", "Invalid organization join code"},
		{authcore.ErrUnavailable, 503, "database_unavailable", "Database is temporarily unavailable"},
		{errors.New("secret-password"), 500, "internal_error", "Internal server error"},
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest("GET", "/", nil)
			handler := NewHandler(nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
			handler.fail(context, "test", tc.err)

			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.message) {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			if tc.status == 503 && response.Header().Get("Retry-After") == "" {
				t.Fatal("503 must include Retry-After")
			}
		})
	}

	var logs bytes.Buffer
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest("GET", "/", nil)
	handler := NewHandler(nil, slog.New(slog.NewJSONHandler(&logs, nil)))
	handler.fail(context, "SQL failed", &pgconn.PgError{Code: "23505", Detail: "secret-join-code"})
	if strings.Contains(logs.String(), "secret-join-code") || strings.Contains(response.Body.String(), "secret-join-code") {
		t.Fatal("PostgreSQL detail leaked")
	}
}
func createRequest(email string) authcore.RegisterRequest {
	return authcore.RegisterRequest{Email: email, Password: "correct horse battery staple", Organization: &authcore.OrganizationInput{Mode: "create", Name: "Demo"}}
}
