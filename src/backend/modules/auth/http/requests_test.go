package http

import (
	"encoding/json"
	authcore "honey-forge/modules/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/platform/httpx"
)

func TestRegistrationValidationBoundaries(t *testing.T) {
	if err := authcore.RegisterValidationRules(); err != nil {
		t.Fatal(err)
	}

	base := func(email, password string, org any) string {
		raw, err := json.Marshal(map[string]any{"email": email, "password": password, "organization": org})
		if err != nil {
			t.Fatal(err)
		}

		return string(raw)
	}
	create := map[string]any{"mode": "create", "name": "A"}

	cases := []struct {
		name, body string
		status     int
	}{
		{"valid trim", base(" A@Example.com ", "demo-password-2026", create), 204},
		{"empty name", base("a@example.com", "demo-password-2026", map[string]any{"mode": "create", "name": " "}), 422},
		{"name 100", base("a@example.com", "demo-password-2026", map[string]any{"mode": "create", "name": strings.Repeat("я", 100)}), 204},
		{"name 101", base("a@example.com", "demo-password-2026", map[string]any{"mode": "create", "name": strings.Repeat("я", 101)}), 422},
		{"password 11", base("a@example.com", strings.Repeat("x", 11), create), 422},
		{"password 12", base("a@example.com", strings.Repeat("x", 12), create), 204},
		{"password 128 unicode", base("a@example.com", strings.Repeat("😀", 128), create), 204},
		{"password 129", base("a@example.com", strings.Repeat("x", 129), create), 422},
		{"unicode email", base("я@example.com", "demo-password-2026", create), 422},
		{"display name", base("Demo <a@example.com>", "demo-password-2026", create), 422},
		{"email 255", base(strings.Repeat("a", 243)+"@example.com", "demo-password-2026", create), 422},
		{"valid join", base("a@example.com", "demo-password-2026", map[string]any{"mode": "join", "join_code": strings.Repeat("x", 32)}), 204},
		{"missing code", base("a@example.com", "demo-password-2026", map[string]any{"mode": "join"}), 422},
		{"invalid code", base("a@example.com", "demo-password-2026", map[string]any{"mode": "join", "join_code": strings.Repeat("!", 32)}), 422},
		{"unknown mode", base("a@example.com", "demo-password-2026", map[string]any{"mode": "other"}), 422},
		{"role injection", `{"email":"a@example.com","password":"demo-password-2026","role":"admin","organization":{"mode":"create","name":"A"}}`, 400},
		{"nested role injection", base("a@example.com", "demo-password-2026", map[string]any{"mode": "create", "name": "A", "role": "admin"}), 400},
		{"join name forbidden", base("a@example.com", "demo-password-2026", map[string]any{"mode": "join", "name": "", "join_code": strings.Repeat("x", 32)}), 400},
		{"nested duplicate", `{"email":"a@example.com","password":"demo-password-2026","organization":{"mode":"create","name":"A","name":"B"}}`, 400},
		{"duplicate", `{"email":"a@example.com","email":"b@example.com"}`, 400},
		{"null", base("a@example.com", "demo-password-2026", nil), 400},
		{"field type", `{"email":1,"password":"demo-password-2026","organization":{"mode":"create","name":"A"}}`, 400},
		{"wrong case", `{"Email":"a@example.com"}`, 400},
		{"missing fields", "{}", 422},
		{"trailing document", "{}{}", 400},
		{"array", "[]", 400},
		{"empty", "", 400},
		{"malformed", "{", 400},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/", bodyLimit, func(c *gin.Context) {
				var req authcore.RegisterRequest
				if bindRequest(c, &req) {
					c.Status(204)
				}
			})

			response := authRequest(router, "POST", "/", tc.body, "", "")
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
			}
			if tc.status == 422 {
				var envelope struct{ Error httpx.Error }
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Error.Code != "validation_failed" || len(envelope.Error.Fields) == 0 || !strings.HasPrefix(envelope.Error.Fields[0].Path, "/") {
					t.Fatal("validation errors require safe JSON pointers")
				}
			}
		})
	}
}

func TestRevisionValidationBoundaries(t *testing.T) {
	if err := authcore.RegisterValidationRules(); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, body string
		status     int
	}{
		{"minimum", `{"expected_revision":1}`, 204},
		{"maximum", `{"expected_revision":2147483647}`, 204},
		{"missing", "{}", 422},
		{"zero", `{"expected_revision":0}`, 422},
		{"negative", `{"expected_revision":-1}`, 422},
		{"over maximum", `{"expected_revision":2147483648}`, 422},
		{"fraction", `{"expected_revision":1.5}`, 400},
		{"wrong type", `{"expected_revision":"1"}`, 400},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/", bodyLimit, func(c *gin.Context) {
				var req authcore.RotateJoinCodeRequest
				if bindRequest(c, &req) {
					c.Status(204)
				}
			})

			response := authRequest(router, "POST", "/", tc.body, "", "")
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
			}
		})
	}
}

func TestMediaTypeAndBodySize(t *testing.T) {
	router := authRouter(t, newMemoryStore())
	cases := []struct {
		name, contentType, body string
		chunked                 bool
		status                  int
	}{
		{"wrong media", "text/plain", "{}", false, 415},
		{"missing media", "", "{}", false, 415},
		{"known size", "application/json", strings.Repeat("x", maxBodySize+1), false, 413},
		{"chunked size", "application/json", strings.Repeat("x", maxBodySize+1), true, 413},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/registrations", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			if tc.chunked {
				req.ContentLength = -1
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
