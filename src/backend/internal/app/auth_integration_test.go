//go:build integration

package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/src/backend/modules/auth"
	authrepo "honey-forge/src/backend/modules/auth/repository"
	authservice "honey-forge/src/backend/modules/auth/service"
	"honey-forge/src/backend/modules/profiles"
	profilerepo "honey-forge/src/backend/modules/profiles/repository"
	profileservice "honey-forge/src/backend/modules/profiles/service"
)

func TestPostgresCompleteRouterFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}

	root, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	var suffix string
	if err := root.QueryRow(t.Context(), "SELECT replace(gen_random_uuid()::text, '-', '')").Scan(&suffix); err != nil {
		t.Fatal(err)
	}
	schema := "router_test_" + suffix
	if _, err := root.Exec(t.Context(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := root.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	files, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		up := strings.TrimPrefix(strings.Split(string(raw), "-- +goose Down")[0], "-- +goose Up")
		if _, err := pool.Exec(t.Context(), up); err != nil {
			t.Fatal(err)
		}
	}

	profileService := profileservice.NewService(profilerepo.NewRepository(pool), profiles.Dependencies{LookupType: lookupProfileType, HasLiveBindings: profilerepo.CheckLiveBindings})
	router, err := NewRouter(authservice.NewService(authrepo.NewRepository(pool)), slog.New(slog.NewJSONHandler(io.Discard, nil)), "https://example.com", profileService, testCatalog())
	if err != nil {
		t.Fatal(err)
	}

	send := func(method, path, body, cookie, csrf string, want int, match ...string) *httptest.ResponseRecorder {
		t.Helper()

		request := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(body))
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("Content-Type", "application/json")
		if len(match) > 0 {
			request.Header.Set("If-Match", match[0])
		}
		if cookie != "" {
			request.AddCookie(&http.Cookie{Name: "__Host-session", Value: cookie})
		}
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s %s: got=%d want=%d body=%s", method, path, response.Code, want, response.Body.String())
		}

		return response
	}

	decodeSession := func(response *httptest.ResponseRecorder) auth.SessionView {
		t.Helper()

		var view auth.SessionView
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}

		return view
	}

	create := `{"email":" Admin@Example.com ","password":"demo-password-2026","organization":{"mode":"create","name":"Demo"}}`
	adminResponse := send("POST", "/api/registrations", create, "", "", 201)
	admin := decodeSession(adminResponse)
	adminCookie := adminResponse.Result().Cookies()[0].Value
	if admin.User.Role != auth.RoleAdmin || admin.User.Email != "admin@example.com" {
		t.Fatal("admin identity incorrect")
	}

	codeResponse := send("GET", "/api/organization/join-code", "", adminCookie, "", 200)
	var code auth.JoinCode
	if err := json.Unmarshal(codeResponse.Body.Bytes(), &code); err != nil {
		t.Fatal(err)
	}
	join := `{"email":"viewer@example.com","password":"demo-password-2026","organization":{"mode":"join","join_code":"` + code.Code + `"}}`
	viewerResponse := send("POST", "/api/registrations", join, "", "", 201)
	viewer := decodeSession(viewerResponse)
	viewerCookie := viewerResponse.Result().Cookies()[0].Value
	if viewer.User.Role != auth.RoleViewer || viewer.Organization.ID != admin.Organization.ID {
		t.Fatal("viewer organization/role incorrect")
	}
	profileBody := `{"request_id":"33333333-3333-4333-8333-333333333333","name":"TCP demo","type_id":"tcp-banner","type_version":1,"config":` + tcpConfig + `}`
	send("POST", "/api/profiles", profileBody, "", "", 401)
	send("POST", "/api/profiles", profileBody, adminCookie, "", 403)
	send("POST", "/api/profiles", profileBody, viewerCookie, viewer.CSRFToken, 403)
	profileResponse := send("POST", "/api/profiles", profileBody, adminCookie, admin.CSRFToken, 201)
	profileLocation := profileResponse.Header().Get("Location")
	profileETag := profileResponse.Header().Get("ETag")
	send("GET", profileLocation, "", viewerCookie, "", 200)
	otherResponse := send("POST", "/api/registrations", strings.Replace(create, "Admin@Example.com", "other@example.com", 1), "", "", 201)
	otherCookie := otherResponse.Result().Cookies()[0].Value
	send("GET", profileLocation, "", otherCookie, "", 404)
	changedProfile := send("PATCH", profileLocation, `{"name":"Updated TCP"}`, adminCookie, admin.CSRFToken, 200, profileETag)
	send("PATCH", profileLocation, `{"name":"Stale TCP"}`, adminCookie, admin.CSRFToken, 412, profileETag)
	profileReplay := send("POST", "/api/profiles", profileBody, adminCookie, admin.CSRFToken, 200)
	if profileReplay.Header().Get("Idempotency-Replayed") != "true" || profileReplay.Header().Get("ETag") != "" || !strings.Contains(profileReplay.Body.String(), `"name":"TCP demo"`) {
		t.Fatal("incorrect profile replay")
	}
	send("GET", "/api/profiles?type_id=tcp-banner&limit=1", "", viewerCookie, "", 200)
	send("DELETE", profileLocation, "", adminCookie, admin.CSRFToken, 204, changedProfile.Header().Get("ETag"))
	send("POST", "/api/profiles", profileBody, adminCookie, admin.CSRFToken, 409)

	send("GET", "/api/organization/join-code", "", viewerCookie, "", 403)
	send("POST", "/api/organization/join-code/rotations", `{"expected_revision":1}`, viewerCookie, viewer.CSRFToken, 403)
	send("POST", "/api/registrations", create, adminCookie, "", 409)
	badLogin := send("POST", "/api/sessions", `{"email":"admin@example.com","password":"wrong-password"}`, adminCookie, "", 401)
	if len(badLogin.Result().Cookies()) != 0 {
		t.Fatal("failed login changed cookie")
	}
	send("GET", "/api/session", "", adminCookie, "", 200)

	login := send("POST", "/api/sessions", `{"email":"admin@example.com","password":"demo-password-2026"}`, adminCookie, "", 201)
	current := decodeSession(login)
	newCookie := login.Result().Cookies()[0].Value
	send("GET", "/api/session", "", adminCookie, "", 401)
	reload := decodeSession(send("GET", "/api/session", "", newCookie, "", 200))
	if reload.CSRFToken != current.CSRFToken {
		t.Fatal("page reload must recover the same CSRF token")
	}

	send("POST", "/api/organization/join-code/rotations", `{"expected_revision":1}`, newCookie, "", 403)
	rotation := send("POST", "/api/organization/join-code/rotations", `{"expected_revision":1}`, newCookie, current.CSRFToken, 200)
	if strings.Contains(rotation.Body.String(), code.Code) {
		t.Fatal("rotation retained old code")
	}
	oldJoin := strings.Replace(join, "viewer@example.com", "old-code@example.com", 1)
	send("POST", "/api/registrations", oldJoin, "", "", 422)

	send("DELETE", "/api/session", "", newCookie, "", 403)
	logout := send("DELETE", "/api/session", "", newCookie, current.CSRFToken, 204)
	if logout.Body.Len() != 0 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout response incorrect")
	}
	send("GET", "/api/session", "", newCookie, "", 401)
	send("GET", "/api/session", "", viewerCookie, "", 200)
	send("DELETE", "/api/session", "", newCookie, "", 204)
}
