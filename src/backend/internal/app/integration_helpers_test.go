//go:build integration

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

const integrationOrigin = "https://operator.example"
const integrationPassword = "demo-password-2026"

type operatorSession struct {
	cookie string
	view   auth.SessionView
}

// Each fixture boots the production composition root on a fresh migrated schema.
func openIntegrationRuntime(t *testing.T) *Runtime {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}

	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	schema := "operator_" + strings.ReplaceAll(string(contract.NewID()), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
	})

	schemaDSN := dsn + " search_path=" + schema
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		databaseURL, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		query := databaseURL.Query()
		query.Set("search_path", schema)
		databaseURL.RawQuery = query.Encode()
		schemaDSN = databaseURL.String()
	}

	runtime, err := Open(t.Context(), Config{
		DatabaseURL:    schemaDSN,
		CursorKey:      []byte("integration-cursor-key-32-bytes!"),
		BrowserOrigins: []string{integrationOrigin},
		MigrationsPath: "../../../../migrations",
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)

	return runtime
}

func operatorRequest(t *testing.T, method, path, body string, session operatorSession) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(t.Context())
	req.Header.Set("Origin", integrationOrigin)
	req.Header.Set("Content-Type", "application/json")
	if session.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-session", Value: session.cookie})
	}
	if session.view.CSRFToken != "" {
		req.Header.Set("X-CSRF-Token", session.view.CSRFToken)
	}

	return req
}

func serveOperator(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func sendOperator(t *testing.T, runtime *Runtime, method, path, body string, session operatorSession, want int, match ...string) *httptest.ResponseRecorder {
	t.Helper()

	req := operatorRequest(t, method, path, body, session)
	if len(match) > 0 {
		req.Header.Set("If-Match", match[0])
	}
	w := serveOperator(runtime.Router, req)
	if w.Code != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}

	return w
}

func integrationJSON(t *testing.T, value any) string {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

func decodeIntegration[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response: %v: %s", err, w.Body.String())
	}

	return value
}

func requireOperatorError(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()

	value := decodeIntegration[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, w)
	if value.Error.Code != want {
		t.Fatalf("error code %q, want %q: %s", value.Error.Code, want, w.Body.String())
	}
}

func sessionFromResponse(t *testing.T, w *httptest.ResponseRecorder) operatorSession {
	t.Helper()

	view := decodeIntegration[auth.SessionView](t, w)
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "__Host-session" {
			if cookie.Value == "" || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteStrictMode || view.CSRFToken == "" {
				t.Fatal("session cookie or CSRF contract violated")
			}

			return operatorSession{cookie: cookie.Value, view: view}
		}
	}

	t.Fatal("session cookie missing")
	return operatorSession{}
}

func registerOperator(t *testing.T, runtime *Runtime, email string, organization auth.OrganizationInput) operatorSession {
	t.Helper()

	body := integrationJSON(t, auth.RegisterRequest{Email: email, Password: integrationPassword, Organization: &organization})
	w := sendOperator(t, runtime, "POST", "/api/registrations", body, operatorSession{}, 201)
	return sessionFromResponse(t, w)
}

func catalogProfileRequest(t *testing.T, runtime *Runtime, session operatorSession) profiles.CreateRequest {
	t.Helper()

	listed := sendOperator(t, runtime, "GET", "/api/trap-types", "", session, 200)
	page := decodeIntegration[contract.Page[catalog.CatalogEntry]](t, listed)
	if len(page.Items) != 1 || page.Items[0].TypeID != "tcp-banner" {
		t.Fatalf("unexpected built-in catalog: %s", listed.Body.String())
	}
	entry := page.Items[0]
	path := fmt.Sprintf("/api/trap-types/%s/versions/%d", entry.TypeID, entry.TypeVersion)
	detail := sendOperator(t, runtime, "GET", path, "", session, 200)
	typ := decodeIntegration[catalog.CatalogEntry](t, detail)
	if detail.Header().Get("ETag") == "" || detail.Header().Get("ETag") != listed.Header().Get("ETag") || len(typ.ConfigSchema) == 0 || !typ.AvailableForNewProfiles {
		t.Fatal("catalog version or snapshot contract violated")
	}

	return profiles.CreateRequest{
		RequestID: string(contract.NewID()), Name: "TCP demo",
		TypeID: string(typ.TypeID), TypeVersion: int64(typ.TypeVersion),
		Config: validIntegrationConfig(),
	}
}

func validIntegrationConfig() profiles.Object {
	return profiles.Object{
		"listeners":  []any{profiles.Object{"name": "ssh", "port": 2222, "banner": "demo", "close_after_banner": true}},
		"logging":    profiles.Object{"capture_payload": false, "max_payload_bytes": 0},
		"management": profiles.Object{"heartbeat_interval_seconds": 10, "telemetry_flush_interval_ms": 500},
	}
}
