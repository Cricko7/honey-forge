package contract

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBrowserGuard(t *testing.T) {
	origin := "https://operator.example"
	policy, err := NewBrowserPolicy([]string{origin})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, role, origin, csrf string
		status                   int
	}{{"anonymous", "", origin, "token", 401}, {"wrong origin", "admin", "https://evil.example", "token", 403}, {"missing csrf", "admin", origin, "", 403}, {"viewer", "viewer", origin, "token", 403}, {"admin", "admin", origin, "token", 204}} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware())
			r.POST("/", func(c *gin.Context) {
				if tt.role != "" {
					SetPrincipal(c, Principal{Role: Role(tt.role)})
				}
				if policy.Guard(c, "token", WriteResources) {
					c.Status(204)
				}
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/", nil)
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("X-CSRF-Token", tt.csrf)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestPermissions(t *testing.T) {
	for _, role := range []Role{Admin, Viewer, Agent} {
		for _, cap := range []Capability{ReadResources, WriteResources, ManageCredentials, FrontendStream, AgentStream} {
			ctx := WithPrincipal(context.Background(), Principal{Role: role})
			want := role == Admin && cap != AgentStream || role == Viewer && (cap == ReadResources || cap == FrontendStream) || role == Agent && cap == AgentStream
			if (AuthorizeCapability(ctx, cap) == nil) != want {
				t.Fatalf("%s %s", role, cap)
			}
		}
	}
}

func TestRESTBodyWithoutBinding(t *testing.T) {
	for _, tt := range []struct {
		name, body, media string
		status            int
	}{{"empty", "", "", 204}, {"json", "{}", "application/json", 204}, {"media", "{}", "text/plain", 415}, {"size", strings.Repeat("x", MaxBodyBytes+1), "application/json", 413}} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware(), RESTBody())
			r.POST("/", func(c *gin.Context) { c.Status(204) })
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.media)
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("%d", w.Code)
			}
		})
	}
}
