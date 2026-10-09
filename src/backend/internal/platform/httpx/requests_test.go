package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBindJSONMediaAndBodyLimits(t *testing.T) {
	for _, tc := range []struct {
		name, media, body string
		chunked           bool
		status            int
	}{
		{"valid", "application/json", `{"name":"demo"}`, false, 204},
		{"empty missing media", "", "", false, 400},
		{"empty JSON", "application/json", "", false, 400},
		{"nonempty missing media", "", `{}`, false, 415},
		{"wrong media", "text/plain", `{}`, false, 415},
		{"chunked too large", "application/json", strings.Repeat("x", MaxBodySize+1), true, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/", BodyLimit, func(c *gin.Context) {
				var req struct {
					Name string `json:"name" binding:"required"`
				}
				if BindJSON(c, &req, nil) {
					c.Status(204)
				}
			})
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.media)
			if tc.chunked {
				req.ContentLength = -1
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
