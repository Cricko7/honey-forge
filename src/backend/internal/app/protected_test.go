package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

func TestProtectedRouteOrder(t *testing.T) {
	policy, err := contract.NewBrowserPolicy([]string{"https://operator.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name          string
		authenticated bool
		status        int
	}{{"unauthenticated malformed body", false, 401}, {"authenticated malformed body", true, 400}} {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter()
			RegisterOperator(r, policy, "POST", "/api/test", contract.WriteResources, func(c *gin.Context) (contract.Principal, string, error) {
				if !tt.authenticated {
					return contract.Principal{}, "", contract.NewError("unauthenticated")
				}
				return contract.Principal{UserID: contract.NewID(), OrganizationID: contract.NewID(), Role: contract.Admin}, "csrf", nil
			}, func(c *gin.Context) {
				var in struct {
					Name string `json:"name" validate:"required"`
				}
				if contract.BindJSON(c, &in) {
					c.Status(204)
				}
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/test", strings.NewReader("invalid"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://operator.example")
			req.Header.Set("X-CSRF-Token", "csrf")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
