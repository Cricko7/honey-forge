package contract

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResponseRules(t *testing.T) {
	for _, tt := range []struct {
		name, match string
		status      int
		cache       string
	}{{"catalog", "", 200, "private, max-age=0, must-revalidate"}, {"catalog cached", `"catalog:1"`, 304, "private, max-age=0, must-revalidate"}, {"catalog weak", `W/"catalog:1"`, 304, "private, max-age=0, must-revalidate"}, {"catalog list", `"other", "catalog:1"`, 304, "private, max-age=0, must-revalidate"}, {"created", "", 201, "no-store"}, {"replayed", "", 200, "no-store"}, {"empty", "", 204, "no-store"}} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware())
			r.GET("/", func(c *gin.Context) {
				switch tt.name {
				case "created":
					CreatedOrReplayed(c, "/api/profiles/11111111-1111-4111-8111-111111111111", map[string]string{"id": "x"}, false)
				case "replayed":
					CreatedOrReplayed(c, "/api/profiles/11111111-1111-4111-8111-111111111111", map[string]string{"id": "x"}, true)
				case "empty":
					NoContent(c)
				default:
					CatalogResponse(c, `"catalog:1"`, map[string]int{"version": 1})
				}
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("If-None-Match", tt.match)
			r.ServeHTTP(w, req)
			if w.Code != tt.status || w.Header().Get("Cache-Control") != tt.cache {
				t.Fatalf("%d %s", w.Code, w.Header())
			}
			if tt.status == 201 && w.Header().Get("Location") == "" {
				t.Fatal("missing Location")
			}
			if tt.status == 204 || tt.status == 304 {
				if w.Body.Len() != 0 {
					t.Fatal("nonempty response")
				}
			}
		})
	}
}

func TestSafeErrorFields(t *testing.T) {
	r := gin.New()
	r.Use(Middleware())
	r.GET("/", func(c *gin.Context) {
		e := NewError("validation_failed")
		for i := 0; i < 25; i++ {
			e.Fields = append(e.Fields, FieldError{Path: "/field", Code: "required", Message: "Field is required"})
		}
		Fail(c, e)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if strings.Count(w.Body.String(), `"path"`) != 20 {
		t.Fatal("field error limit")
	}
}
