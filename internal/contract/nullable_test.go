package contract

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequiredNullable(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
	}{{"missing", `{}`, 422}, {"null", `{"value":null}`, 200}, {"value", `{"value":"x"}`, 200}, {"wrong type", `{"value":1}`, 400}} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware())
			r.POST("/", func(c *gin.Context) {
				var in struct {
					Value Nullable[string] `json:"value" validate:"present"`
				}
				if BindJSON(c, &in) {
					c.JSON(200, in)
				}
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
