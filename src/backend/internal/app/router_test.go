package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRouterErrors(t *testing.T) {
	r := NewRouter()
	r.GET("/api/resource", func(c *gin.Context) { c.Status(204) })
	r.GET("/api/panic", func(c *gin.Context) { panic("secret") })
	for _, tt := range []struct {
		name, method, path, code string
		status                   int
	}{{"absent", "GET", "/api/absent", "resource_not_found", 404}, {"method", "POST", "/api/resource", "method_not_allowed", 405}, {"panic", "GET", "/api/panic", "internal_error", 500}} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
			if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.code) || strings.Contains(w.Body.String(), "secret") || w.Header().Get("X-Request-ID") == "" {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
