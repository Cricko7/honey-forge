//go:build integration

package app

import (
	"context"
	"encoding/json"
	"honey-forge/internal/contract"
	"honey-forge/internal/postgres"
	"honey-forge/modules/catalog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRealSessionCatalogAndProfileFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := "operator_" + strings.ReplaceAll(string(contract.NewID()), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	isolated, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	if err := postgres.Migrate(t.Context(), isolated, os.DirFS("../../../migrations")); err != nil {
		t.Fatal(err)
	}
	// startup must use the same isolated schema as these real auth repositories.
	databaseURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := databaseURL.Query()
	query.Set("search_path", schema)
	databaseURL.RawQuery = query.Encode()
	schemaDSN := databaseURL.String()
	runtime, err := Open(t.Context(), Config{DatabaseURL: schemaDSN, CursorKey: make([]byte, 32), BrowserOrigins: []string{"https://operator.example"}, MigrationsPath: "../../../migrations"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	send := func(method, path, body, cookie, csrf string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Origin", "https://operator.example")
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "__Host-session", Value: cookie})
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		runtime.Router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	send("GET", "/api/trap-types", "", "", "", 401)
	created := send("POST", "/api/registrations", `{"email":"admin@example.com","password":"demo-password-2026","organization":{"mode":"create","name":"Demo"}}`, "", "", 201)
	adminCookie := created.Result().Cookies()[0].Value
	var admin struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &admin); err != nil {
		t.Fatal(err)
	}
	listed := send("GET", "/api/trap-types", "", adminCookie, "", 200)
	var page contract.Page[catalog.CatalogEntry]
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TypeID != "tcp-banner" {
		t.Fatal(listed.Body.String())
	}
	detail := send("GET", "/api/trap-types/tcp-banner/versions/1", "", adminCookie, "", 200)
	if detail.Header().Get("ETag") != listed.Header().Get("ETag") {
		t.Fatal("different catalog snapshot")
	}
	code := send("GET", "/api/organization/join-code", "", adminCookie, "", 200)
	var invite struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(code.Body.Bytes(), &invite); err != nil {
		t.Fatal(err)
	}
	joined := send("POST", "/api/registrations", `{"email":"viewer@example.com","password":"demo-password-2026","organization":{"mode":"join","join_code":"`+invite.Code+`"}}`, "", "", 201)
	viewerCookie := joined.Result().Cookies()[0].Value
	var viewer struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(joined.Body.Bytes(), &viewer); err != nil {
		t.Fatal(err)
	}
	send("GET", "/api/trap-types", "", viewerCookie, "", 200)
	configJSON := `{"listeners":[{"name":"ssh","port":2222.0,"banner":"demo","close_after_banner":true}],"logging":{"capture_payload":false,"max_payload_bytes":0},"management":{"heartbeat_interval_seconds":10,"telemetry_flush_interval_ms":500}}`
	profileJSON := `{"request_id":"33333333-3333-4333-8333-333333333333","name":"TCP demo","description":"","type_id":"tcp-banner","type_version":1,"config":` + configJSON + `}`
	send("POST", "/api/profiles", profileJSON, adminCookie, "", 403)
	send("POST", "/api/profiles", profileJSON, viewerCookie, viewer.CSRF, 403)
	profile := send("POST", "/api/profiles", profileJSON, adminCookie, admin.CSRF, 201)
	send("GET", profile.Header().Get("Location"), "", viewerCookie, "", 200)
	invalid := strings.Replace(profileJSON, `33333333-3333-4333-8333-333333333333`, `44444444-4444-4444-8444-444444444444`, 1)
	invalid = strings.Replace(invalid, `2222.0`, `0`, 1)
	rejected := send("POST", "/api/profiles", invalid, adminCookie, admin.CSRF, 422)
	if !strings.Contains(rejected.Body.String(), `"code":"config_invalid"`) {
		t.Fatal(rejected.Body.String())
	}
	send("DELETE", "/api/session", "", adminCookie, admin.CSRF, 204)
	req := httptest.NewRequest("GET", "/api/trap-types", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-session", Value: adminCookie})
	req.Header.Set("If-None-Match", "*")
	w := httptest.NewRecorder()
	runtime.Router.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("revoked session can read/304: %d", w.Code)
	}
}
