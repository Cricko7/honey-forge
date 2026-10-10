package contract

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRevisionBinding(t *testing.T) {
	r := gin.New()
	r.Use(Middleware())
	r.POST("/", func(c *gin.Context) {
		var in struct {
			Revision Revision `json:"revision" validate:"min=1,max=2147483647"`
		}
		if BindJSON(c, &in) {
			c.Status(200)
		}
	})
	for _, tt := range []struct {
		name, body string
		status     int
	}{{"maximum", `{"revision":2147483647}`, 200}, {"out of range", `{"revision":2147483648}`, 422}, {"missing", `{}`, 422}, {"fraction", `{"revision":1.5}`, 400}} {
		t.Run(tt.name, func(t *testing.T) {
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

func TestOrganizationIsolation(t *testing.T) {
	id := ID("11111111-1111-4111-8111-111111111111")
	ctx := WithPrincipal(context.Background(), Principal{OrganizationID: id, Role: Viewer})
	if err := RequireOrganization(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := RequireOrganization(ctx, "22222222-2222-4222-8222-222222222222"); err == nil || err.(*Error).Code != "resource_not_found" {
		t.Fatalf("foreign access: %v", err)
	}
	if err := RequireOrganization(context.Background(), id); err == nil || err.(*Error).Code != "unauthenticated" {
		t.Fatalf("anonymous access: %v", err)
	}
}

func TestEnvelopeNotification(t *testing.T) {
	b, err := json.Marshal(Envelope{MessageID: NewID(), Type: "trap.changed", Payload: map[string]json.RawMessage{}})
	if err != nil || !strings.Contains(string(b), `"reply_to":null`) {
		t.Fatalf("notification %s %v", b, err)
	}
}

func TestConcurrentRequests(t *testing.T) {
	r := gin.New()
	r.Use(Middleware())
	r.GET("/", func(c *gin.Context) { c.Status(204) })
	var wg sync.WaitGroup
	ids := make(chan string, 32)
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != 204 || w.Body.Len() != 0 {
				t.Errorf("empty response %d %s", w.Code, w.Body.String())
			}
			ids <- w.Header().Get("X-Request-ID")
		})
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if !ValidID(id) || seen[id] {
			t.Fatalf("invalid or repeated request identifier %q", id)
		}
		seen[id] = true
	}
}
