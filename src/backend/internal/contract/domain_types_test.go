package contract

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDomainTypes(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
	}{{"normalize", `{"name":"  trap  ","id":"11111111-1111-4111-8111-111111111111","type_id":"tcp-banner","description":""}`, 200}, {"description missing", `{"name":"trap","id":"11111111-1111-4111-8111-111111111111","type_id":"tcp-banner"}`, 422}, {"name whitespace", `{"name":"  ","id":"11111111-1111-4111-8111-111111111111","type_id":"tcp-banner","description":""}`, 422}, {"invalid id", `{"name":"trap","id":"wrong","type_id":"tcp-banner","description":""}`, 422}, {"invalid type", `{"name":"trap","id":"11111111-1111-4111-8111-111111111111","type_id":"TCP","description":""}`, 422}, {"description null", `{"name":"trap","id":"11111111-1111-4111-8111-111111111111","type_id":"tcp-banner","description":null}`, 400}} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware())
			r.POST("/", func(c *gin.Context) {
				var in struct {
					Name        Name        `json:"name" validate:"required"`
					ID          ID          `json:"id" validate:"required"`
					TypeID      TypeID      `json:"type_id" validate:"required"`
					Description Description `json:"description" validate:"present"`
				}
				if BindJSON(c, &in) {
					if in.Name != "trap" {
						t.Error("name not normalized")
					}
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
