package catalog

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"

	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
)

func TestOpenAPIResponse(t *testing.T) {
	raw, err := os.ReadFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	root := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "components": document["components"]}
	for _, tt := range []struct{ name, path, ref string }{
		{"entry", "/api/trap-types/tcp-banner/versions/1", "CatalogEntry"},
		{"page", "/api/trap-types", "CatalogPage"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root["$ref"] = "#/components/schemas/" + tt.ref
			schemaRaw, err := json.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			schema, err := configschema.Compile(schemaRaw)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			testRouter(t).ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if err := schema.Validate(w.Body.Bytes(), ""); err != nil {
				t.Fatalf("response violates OpenAPI: %v", err)
			}
		})
	}
}

func testRouter(t *testing.T, defs ...Definition) *gin.Engine {
	t.Helper()
	s := testService(t, defs...)
	r := gin.New()
	r.Use(contract.Middleware(), func(c *gin.Context) {
		contract.SetPrincipal(c, contract.Principal{Role: contract.Viewer, OrganizationID: "11111111-1111-4111-8111-111111111111"})
		c.Next()
	})
	h := NewHandler(s)
	r.GET("/api/trap-types", h.List)
	r.GET("/api/trap-types/:type_id/versions/:type_version", h.Read)
	return r
}

func TestHandler(t *testing.T) {
	r := testRouter(t)
	for _, tt := range []struct {
		name, path string
		status     int
		code       string
	}{
		{"list", "/api/trap-types", 200, ""},
		{"detail", "/api/trap-types/tcp-banner/versions/1", 200, ""},
		{"unknown version", "/api/trap-types/tcp-banner/versions/2", 404, "resource_not_found"},
		{"unknown type", "/api/trap-types/unknown/versions/1", 404, "resource_not_found"},
		{"invalid id", "/api/trap-types/UPPER/versions/1", 400, "invalid_id"},
		{"zero version", "/api/trap-types/tcp-banner/versions/0", 400, "invalid_id"},
		{"overflow version", "/api/trap-types/tcp-banner/versions/2147483648", 400, "invalid_id"},
		{"fraction version", "/api/trap-types/tcp-banner/versions/1.0", 400, "invalid_id"},
		{"signed version", "/api/trap-types/tcp-banner/versions/+1", 400, "invalid_id"},
		{"available", "/api/trap-types?available_for_new_profiles=true", 200, ""},
		{"unavailable", "/api/trap-types?available_for_new_profiles=false", 200, ""},
		{"type filter", "/api/trap-types?type_id=tcp-banner", 200, ""},
		{"empty type", "/api/trap-types?type_id=", 400, "invalid_query"},
		{"invalid boolean", "/api/trap-types?available_for_new_profiles=1", 400, "invalid_query"},
		{"empty boolean", "/api/trap-types?available_for_new_profiles=", 400, "invalid_query"},
		{"duplicate query", "/api/trap-types?limit=1&limit=2", 400, "invalid_query"},
		{"unknown query", "/api/trap-types?offset=0", 400, "invalid_query"},
		{"invalid encoding", "/api/trap-types?type_id=%xx", 400, "invalid_query"},
		{"invalid limit", "/api/trap-types?limit=101", 400, "invalid_query"},
		{"empty cursor", "/api/trap-types?cursor=", 400, "invalid_cursor"},
		{"forged cursor", "/api/trap-types?cursor=garbage", 400, "invalid_cursor"},
		{"unknown detail query", "/api/trap-types/tcp-banner/versions/1?fields=x", 400, "invalid_query"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
			if w.Code != tt.status {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if tt.code != "" {
				var body contract.ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Code != tt.code {
					t.Fatal(w.Body.String())
				}
			}
		})
	}
}

func TestHandlerConditionalRead(t *testing.T) {
	defs := BuiltinDefinitions()
	second := BuiltinDefinitions()[0]
	second.Entry.TypeVersion = 2
	defs = append(defs, second)
	r := testRouter(t, defs...)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/trap-types?limit=1", nil))
	tag := w.Header().Get("ETag")
	var page contract.Page[CatalogEntry]
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == nil || tag == "" {
		t.Fatal("missing cursor or etag")
	}
	for _, tt := range []struct {
		name, path, etag string
		status           int
	}{
		{"first page", "/api/trap-types?limit=1", tag, 304},
		{"weak ETag", "/api/trap-types", "W/" + tag, 304},
		{"ETag list", "/api/trap-types", `"other", ` + tag, 304},
		{"wildcard", "/api/trap-types", "*", 304},
		{"different tag", "/api/trap-types", `"old"`, 200},
		{"detail", "/api/trap-types/tcp-banner/versions/1", tag, 304},
		{"cursor ignores condition", "/api/trap-types?limit=1&cursor=" + *page.NextCursor, tag, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			req.Header.Set("If-None-Match", tt.etag)
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if out.Code != tt.status {
				t.Fatalf("%d %s", out.Code, out.Body.String())
			}
			if out.Header().Get("ETag") != tag || out.Header().Get("Cache-Control") != "private, max-age=0, must-revalidate" {
				t.Fatal(out.Header())
			}
			if tt.status == 304 && out.Body.Len() != 0 {
				t.Fatal("304 has a body")
			}
		})
	}
}

func TestRepeatedConditionalHeaders(t *testing.T) {
	r := testRouter(t)
	initial := httptest.NewRecorder()
	r.ServeHTTP(initial, httptest.NewRequest("GET", "/api/trap-types", nil))
	for _, path := range []string{"/api/trap-types", "/api/trap-types/tcp-banner/versions/1"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Add("If-None-Match", `"other"`)
		req.Header.Add("If-None-Match", initial.Header().Get("ETag"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 304 || w.Body.Len() != 0 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
