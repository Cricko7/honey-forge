package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

func TestOwnershipBeforeBody(t *testing.T) {
	policy, err := contract.NewBrowserPolicy([]string{"https://operator.example"})
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter()
	org := contract.NewID()
	called := false
	RegisterOperator(r, policy, "PATCH", "/api/profiles/:id", contract.WriteResources, func(c *gin.Context) (contract.Principal, string, error) {
		return contract.Principal{UserID: contract.NewID(), OrganizationID: org, Role: contract.Admin}, "csrf", nil
	}, func(c *gin.Context) {
		called = true
		var in struct {
			Name string `json:"name"`
		}
		contract.BindJSON(c, &in)
	}, func(c *gin.Context) {
		if err := contract.RequireOrganization(c.Request.Context(), contract.NewID()); err != nil {
			contract.Fail(c, err.(*contract.Error))
		}
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/profiles/11111111-1111-4111-8111-111111111111", strings.NewReader("invalid-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://operator.example")
	req.Header.Set("X-CSRF-Token", "csrf")
	r.ServeHTTP(w, req)
	if w.Code != 404 || called {
		t.Fatalf("ownership was checked after input: %d", w.Code)
	}
}
