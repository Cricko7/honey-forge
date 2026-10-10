package contract

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRetryHeaders(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
	}{{"rate_limited", 429}, {"database_unavailable", 503}, {"service_unavailable", 503}} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware())
			r.GET("/", func(c *gin.Context) { Fail(c, NewError(tt.name)) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != tt.status || w.Header().Get("Retry-After") != "1" {
				t.Fatalf("%d %s", w.Code, w.Header())
			}
		})
	}
}
