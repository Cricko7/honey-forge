package agentws

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

func TestAnonymousResource(t *testing.T) {
	gateway := &fakeGateway{token: "secret"}
	router := gin.New()
	router.Use(contract.Middleware())
	NewHandler(gateway, &fakeCommands{}).Register(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()

	var resource string
	for _, tt := range []struct {
		name, token string
		upgrade     bool
	}{
		{name: "ordinary request"},
		{name: "ordinary request with token", token: "secret"},
		{name: "upgrade without token", upgrade: true},
		{name: "upgrade with wrong token", token: "wrong", upgrade: true},
		{name: "malformed bearer", token: "Basic secret", upgrade: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, server.URL+"/assets/stream", nil)
			if err != nil {
				t.Fatal(err)
			}

			if tt.token != "" {
				req.Header.Set("Authorization", tt.token)
				if !strings.HasPrefix(tt.token, "Basic") {
					req.Header.Set("Authorization", "Bearer "+tt.token)
				}
			}

			if tt.upgrade {
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Upgrade", "websocket")
			}

			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}

			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}

			if response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/javascript; charset=utf-8" {
				t.Fatalf("unexpected resource: %d %s", response.StatusCode, body)
			}

			if resource == "" {
				resource = string(body)
			}

			if string(body) != resource || strings.Contains(string(body), "agent") || strings.Contains(string(body), "secret") {
				t.Fatalf("distinguishable resource: %s", body)
			}
		})
	}

	if len(gateway.observed) != 0 || len(gateway.batches) != 0 {
		t.Fatal("resource requests caused agent side effects")
	}
}

func TestResourceHead(t *testing.T) {
	router := gin.New()
	NewHandler(nil, nil).Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/assets/stream", nil))
	if response.Code != 200 || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "application/javascript; charset=utf-8" || response.Header().Get("Content-Length") == "" {
		t.Fatalf("invalid HEAD resource: %d %v %s", response.Code, response.Header(), response.Body.String())
	}
}
