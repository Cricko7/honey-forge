package contract

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBinding(t *testing.T) {
	tests := []struct {
		name, body, media string
		status            int
	}{
		{"success", `{"name":"demo"}`, "application/json", 200},
		{"missing", `{}`, "application/json", 422},
		{"unknown", `{"name":"x","extra":1}`, "application/json", 400},
		{"duplicate", `{"name":"x","name":"y"}`, "application/json", 400},
		{"nested duplicate", `{"name":"x","data":{"a":1,"a":2}}`, "application/json", 400},
		{"two documents", `{"name":"x"} {}`, "application/json", 400},
		{"wrong type", `{"name":1}`, "application/json", 400},
		{"scalar null", `{"name":null}`, "application/json", 400},
		{"wrong case", `{"Name":"x"}`, "application/json", 400},
		{"null", `null`, "application/json", 400},
		{"empty", ``, "application/json", 400},
		{"media", `{}`, "text/plain", 415},
		{"too large", strings.Repeat(" ", MaxBodyBytes+1), "application/json", 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware())
			r.POST("/api/test", func(c *gin.Context) {
				var in struct {
					Name string         `json:"name" validate:"required,name"`
					Data map[string]any `json:"data"`
				}
				if BindJSON(c, &in) {
					c.JSON(200, in)
				}
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/test", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.media)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if !ValidID(w.Header().Get("X-Request-ID")) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing common headers")
			}
			if tt.status >= 400 && !strings.Contains(w.Body.String(), w.Header().Get("X-Request-ID")) {
				t.Fatal("request ID mismatch")
			}
		})
	}
}

func TestPreconditions(t *testing.T) {
	id := ID("11111111-1111-4111-8111-111111111111")
	for _, tt := range []struct {
		header string
		status int
	}{{"", 428}, {"*", 400}, {ProfileETag(id, 1), 200}, {ProfileETag(id, 2), 412}} {
		r := gin.New()
		r.Use(Middleware())
		r.PATCH("/", func(c *gin.Context) {
			if CheckProfileRevision(c, id, 1) {
				c.Status(200)
			}
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("PATCH", "/", nil)
		if tt.header != "" {
			req.Header.Set("If-Match", tt.header)
		}
		r.ServeHTTP(w, req)
		if w.Code != tt.status {
			t.Fatalf("%q: %d", tt.header, w.Code)
		}
	}
}

func TestAccess(t *testing.T) {
	for _, tt := range []struct {
		role   Role
		status int
	}{{"", 401}, {Viewer, 403}, {Agent, 403}, {Admin, 200}} {
		r := gin.New()
		r.Use(Middleware())
		r.POST("/", func(c *gin.Context) {
			if tt.role != "" {
				SetPrincipal(c, Principal{Role: tt.role})
			}
			if RequireRole(c, Admin) {
				c.Status(200)
			}
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
		if w.Code != tt.status {
			t.Fatalf("%s: %d", tt.role, w.Code)
		}
	}
}
