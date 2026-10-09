package http

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"honey-forge/src/backend/modules/auth"
	profilecore "honey-forge/src/backend/modules/profiles"
	profileservice "honey-forge/src/backend/modules/profiles/service"
)

func httpRouter(t *testing.T, actor auth.AuthContext) (*gin.Engine, *profileservice.Service) {
	t.Helper()
	s, _ := testService()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Header("Cache-Control", "no-store")
	})
	h := NewHandler(s, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err := h.RegisterRoutes(r, func(c *gin.Context) { c.Set("auth_context", actor); c.Next() }); err != nil {
		t.Fatal(err)
	}

	return r, s
}

func call(r http.Handler, method, path, body, match string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if match != "" {
		req.Header.Set("If-Match", match)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHTTPProfileFlow(t *testing.T) {
	r, _ := httpRouter(t, admin)
	w := call(r, "POST", "/api/profiles", mustJSON(request()), "")
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	var p profilecore.Profile
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}

	location := w.Header().Get("Location")
	etag := w.Header().Get("ETag")
	if location != "/api/profiles/"+p.ID || etag != profilecore.ETag(p) || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("bad create headers/secrets: location=%q etag=%q want=%q body=%s", location, etag, profilecore.ETag(p), w.Body.String())
	}

	w = call(r, "GET", location, "", "")
	if w.Code != 200 || w.Header().Get("ETag") != etag {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}

	w = call(r, "PATCH", location, `{"name":"Changed"}`, etag)
	if w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}

	current := w.Header().Get("ETag")
	w = call(r, "POST", "/api/profiles", mustJSON(request()), "")
	if w.Code != 200 || w.Header().Get("Idempotency-Replayed") != "true" || w.Header().Get("ETag") != "" {
		t.Fatalf("replay: %d %s", w.Code, w.Body.String())
	}

	w = call(r, "PATCH", location, `{"name":"Changed"}`, current)
	if w.Code != 200 || w.Header().Get("ETag") != current {
		t.Fatal("noop changed revision")
	}

	w = call(r, "DELETE", location, "", current)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}

	w = call(r, "GET", location, "", "")
	if w.Code != 404 {
		t.Fatalf("deleted get: %d", w.Code)
	}
}

func TestHTTPProfileValidation(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, etag string
		status                         int
		code                           string
	}{
		{
			"missing request ID", "POST", "/api/profiles",
			`{"name":"a","type_id":"demo","type_version":1,"config":{}}`,
			"", 422, "validation_failed",
		},
		{
			"type version range", "POST", "/api/profiles",
			`{"request_id":"` + requestID + `","name":"a","type_id":"demo","type_version":2147483648,"config":{}}`,
			"", 422, "validation_failed",
		},
		{"unknown key", "POST", "/api/profiles", `{"role":"admin"}`, "", 400, "invalid_json"},
		{"duplicate nested", "POST", "/api/profiles", `{"config":{"a":1,"a":2}}`, "", 400, "invalid_json"},
		{"case key", "POST", "/api/profiles", `{"Name":"a"}`, "", 400, "invalid_json"},
		{"null", "POST", "/api/profiles", `{"description":null}`, "", 400, "invalid_json"},
		{"array config", "POST", "/api/profiles", `{"config":[]}`, "", 400, "invalid_json"},
		{"multiple documents", "POST", "/api/profiles", `{} {}`, "", 400, "invalid_json"},
		{"body size", "POST", "/api/profiles", strings.Repeat("x", (256<<10)+1), "", 413, "body_too_large"},
		{"bad path ID", "GET", "/api/profiles/nope", "", "", 400, "invalid_id"},
		{"unknown query", "GET", "/api/profiles?sort=name", "", "", 400, "invalid_query"},
		{"duplicate query", "GET", "/api/profiles?limit=1&limit=2", "", "", 400, "invalid_query"},
		{"bad cursor", "GET", "/api/profiles?cursor=garbage", "", "", 400, "invalid_cursor"},
		{"bad limit", "GET", "/api/profiles?limit=0", "", "", 400, "invalid_query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := httpRouter(t, admin)
			w := call(r, tc.method, tc.path, tc.body, tc.etag)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}

			if !strings.Contains(w.Body.String(), `"request_id":"`+requestID+`"`) {
				t.Fatal("missing request ID")
			}
		})
	}
}

func TestHTTPProfilePreconditionsAndAccess(t *testing.T) {
	for _, tc := range []struct {
		name, body, etag string
		status           int
		code             string
	}{
		{"missing", `{"name":"X"}`, "", 428, "precondition_required"},
		{"wildcard", `{"name":"X"}`, "*", 400, "invalid_precondition"},
		{"weak", `{"name":"X"}`, `W/"profile:ID:1"`, 400, "invalid_precondition"},
		{"empty patch", `{}`, "valid", 422, "validation_failed"},
		{"immutable type", `{"type_id":"other"}`, "valid", 400, "invalid_json"},
		{
			"duplicate clears",
			`{"clear_secret_fields":["/credentials/password","/credentials/password"]}`,
			"valid", 422, "validation_failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, s := httpRouter(t, admin)
			c, e := s.Create(t.Context(), admin, request())
			if e != nil {
				t.Fatal(e)
			}

			match := tc.etag
			if match == "valid" {
				match = profilecore.ETag(c.Profile)
			}

			w := call(r, "PATCH", "/api/profiles/"+c.Profile.ID, tc.body, match)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}

	viewer := admin
	viewer.Role = auth.RoleViewer
	r, s := httpRouter(t, viewer)
	c, e := s.Create(t.Context(), admin, request())
	if e != nil {
		t.Fatal(e)
	}

	if w := call(r, "GET", "/api/profiles/"+c.Profile.ID, "", ""); w.Code != 200 {
		t.Fatal("viewer read rejected")
	}

	if w := call(r, "POST", "/api/profiles", `broken`, ""); w.Code != 403 {
		t.Fatal("viewer write did not precede validation")
	}

	r, _ = httpRouter(t, auth.AuthContext{})
	if w := call(r, "GET", "/api/profiles/nope", "", ""); w.Code != 401 {
		t.Fatal("unauthenticated request probed resource")
	}
}

const (
	orgID     = "11111111-1111-4111-8111-111111111111"
	userID    = "22222222-2222-4222-8222-222222222222"
	requestID = "33333333-3333-4333-8333-333333333333"
)

var admin = auth.AuthContext{UserID: userID, OrganizationID: orgID, Role: auth.RoleAdmin}

func request() profilecore.CreateRequest {
	return profilecore.CreateRequest{RequestID: requestID, Name: "Demo", TypeID: "demo", TypeVersion: 1, Config: profilecore.Object{"visible": "one", "credentials": profilecore.Object{"password": "private"}}}
}
func testService() (*profileservice.Service, *fakeStore) {
	store := &fakeStore{profiles: map[string]profilecore.Profile{}, keys: map[string]fakeKey{}, snapshots: map[string]profilecore.Profile{}}
	deps := profilecore.Dependencies{LookupType: func(ctx context.Context, id string, version int32) (profilecore.Type, error) {
		return profilecore.Type{InteractionLevel: "low", AvailableForNewProfiles: true, SecretPaths: func(profilecore.Object) []string { return []string{"/credentials/password"} }, IsSecretPath: func(p string) bool { return p == "/credentials/password" }, CheckConfig: func(ctx context.Context, _ profilecore.Object) error { return ctx.Err() }}, nil
	}, HasLiveBindings: func(ctx context.Context, _, _ string, _ pgx.Tx) (bool, error) { return false, ctx.Err() }}
	return profileservice.NewService(store, deps), store
}
