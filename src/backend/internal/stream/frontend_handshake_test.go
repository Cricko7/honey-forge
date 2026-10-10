package stream

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
)

func TestFrontendHandshake(t *testing.T) {
	policy, err := contract.NewBrowserPolicy([]string{"https://operator.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, origin, protocol string
		role                   contract.Role
		status                 int
	}{
		{"admin", "https://operator.example", "dashboard-stream.v1", contract.Admin, 101},
		{"viewer", "https://operator.example", "dashboard-stream.v1", contract.Viewer, 101},
		{"missing origin", "", "dashboard-stream.v1", contract.Admin, 403},
		{"foreign origin", "https://evil.example", "dashboard-stream.v1", contract.Admin, 403},
		{"missing protocol", "https://operator.example", "", contract.Admin, 400},
		{"wrong protocol", "https://operator.example", "resource-stream.v1", contract.Admin, 400},
		{"wrong role", "https://operator.example", "dashboard-stream.v1", contract.Agent, 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(contract.Middleware())
			r.GET(FrontendPath, func(c *gin.Context) {
				contract.SetPrincipal(c, contract.Principal{Role: tt.role})
				s, e := Upgrade(c, contract.FrontendStream, policy)
				if e == nil {
					defer s.Close()
				}
			})
			server := httptest.NewTLSServer(r)
			defer server.Close()
			d := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
			if tt.protocol != "" {
				d.Subprotocols = []string{tt.protocol}
			}
			h := http.Header{}
			if tt.origin != "" {
				h.Set("Origin", tt.origin)
			}
			c, res, e := d.Dial("wss"+strings.TrimPrefix(server.URL, "https")+FrontendPath, h)
			if c != nil {
				defer c.Close()
			}
			if res == nil {
				t.Fatal(e)
			}
			defer res.Body.Close()
			if res.StatusCode != tt.status {
				t.Fatalf("status %d want %d", res.StatusCode, tt.status)
			}
			if tt.status == 101 && c.Subprotocol() != "dashboard-stream.v1" {
				t.Fatal("subprotocol not negotiated")
			}
		})
	}
}
