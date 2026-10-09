package app

import (
	"encoding/json"
	"honey-forge/internal/catalog"
	"honey-forge/internal/contract"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCatalogAccess(t *testing.T) {
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	browser, err := contract.NewBrowserPolicy([]string{"https://operator.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		role   contract.Role
		status int
		code   string
	}{
		{"admin", contract.Admin, 200, ""}, {"viewer", contract.Viewer, 200, ""}, {"agent", contract.Agent, 403, "forbidden"}, {"no session", "", 401, "unauthenticated"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter()
			RegisterCatalog(r, browser, func(*gin.Context) (contract.Principal, string, error) {
				if tt.role == "" {
					return contract.Principal{}, "", contract.NewError("unauthenticated")
				}
				return contract.Principal{OrganizationID: contract.NewID(), Role: tt.role}, "", nil
			}, service)
			for _, path := range []string{"/api/trap-types", "/api/trap-types/tcp-banner/versions/1"} {
				w := httptest.NewRecorder()
				req := httptest.NewRequest("GET", path, nil)
				req.Header.Set("If-None-Match", "*")
				r.ServeHTTP(w, req)
				want := tt.status
				if want == 200 {
					want = 304
				}
				if w.Code != want {
					t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
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
			}
		})
	}
	r := NewRouter()
	RegisterCatalog(r, browser, nil, service)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/trap-types", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/trap-types", nil))
	if w.Code != 405 {
		t.Fatalf("catalog CRUD exposed: %d", w.Code)
	}
}
