package traplinks

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

func TestHTTPBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, method, body string
		role               contract.Role
		status             int
	}{
		{"list", "GET", "", contract.Viewer, 200},
		{"create", "POST", `{"source_id":"` + sourceID + `","target_id":"` + targetID + `"}`, contract.Admin, 201},
		{"viewer", "POST", `{}`, contract.Viewer, 403},
		{"malformed", "POST", `{`, contract.Admin, 400},
		{"unknown field", "POST", `{"extra":true}`, contract.Admin, 400},
		{"missing target", "POST", `{"source_id":"` + sourceID + `"}`, contract.Admin, 422},
		{"self link", "POST", `{"source_id":"` + sourceID + `","target_id":"` + sourceID + `"}`, contract.Admin, 422},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(contract.Middleware())
			session := func(c *gin.Context) {
				contract.SetPrincipal(c, contract.Principal{Role: tt.role, OrganizationID: orgID})
				c.Next()
			}
			NewHandler(NewService(&fakeStore{links: []Link{}}), slog.Default()).Register(router, session)
			req := httptest.NewRequest(tt.method, "/api/trap-links", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
			}
			if tt.status == 201 && w.Header().Get("Location") != "/api/trap-links/"+sourceID+"/"+targetID {
				t.Fatal("missing Location")
			}
			if tt.status == 200 && w.Body.String() != `{"items":[]}` {
				t.Fatal(w.Body.String())
			}
		})
	}
}
