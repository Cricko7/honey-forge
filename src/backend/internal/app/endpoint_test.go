package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAgentEndpointConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, agent, public string
		browsers            []string
		valid               bool
	}{
		{name: "separate hosts", agent: "wss://resources.example/assets/stream", public: "https://api.example", browsers: []string{"https://app.example"}, valid: true},
		{name: "missing agent", public: "https://api.example"},
		{name: "missing public", agent: "wss://resources.example/assets/stream"},
		{name: "operator host", agent: "wss://api.example/assets/stream", public: "https://api.example"},
		{name: "operator host with different case and port", agent: "wss://API.EXAMPLE:8443/assets/stream", public: "https://api.example"},
		{name: "browser host", agent: "wss://app.example/assets/stream", public: "https://api.example", browsers: []string{"https://app.example"}},
		{name: "second browser host", agent: "wss://app.example/assets/stream", public: "https://api.example", browsers: []string{"https://other.example", "https://app.example"}},
		{name: "query", agent: "wss://resources.example/assets/stream?", public: "https://api.example"},
		{name: "plaintext", agent: "ws://resources.example/assets/stream", public: "https://api.example"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := configureAgentEndpoint(tt.agent, tt.public, tt.browsers)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, err)
			}
		})
	}
}

func TestTelemetryHostIsolation(t *testing.T) {
	endpoint, err := configureAgentEndpoint("wss://resources.example/assets/stream", "https://api.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(telemetryHost(endpoint))
	router.GET("/api/session", func(c *gin.Context) { c.String(200, "operator") })
	router.GET("/assets/stream", func(c *gin.Context) { c.String(200, "authorized stream boundary") })
	for _, tt := range []struct {
		name, host, method, path, contains string
		status                             int
	}{
		{"cover website", "resources.example", "GET", "/", "Resource Library", 200},
		{"operator hidden", "resources.example", "GET", "/api/session", "Not Found", 404},
		{"frontend hidden", "resources.example", "GET", "/api/stream", "Not Found", 404},
		{"unknown resource", "resources.example", "GET", "/unknown", "Not Found", 404},
		{"wrong method", "resources.example", "POST", "/assets/stream", "Not Found", 404},
		{"agent on operator host", "api.example", "GET", "/assets/stream", "Not Found", 404},
		{"forwarded host cannot unlock", "api.example", "GET", "/assets/stream", "Not Found", 404},
		{"stream on telemetry host", "RESOURCES.EXAMPLE:443", "GET", "/assets/stream", "authorized stream boundary", 200},
		{"operator on operator host", "api.example", "GET", "/api/session", "operator", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			request.Host = tt.host
			request.Header.Set("X-Forwarded-Host", "resources.example")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.status || !strings.Contains(response.Body.String(), tt.contains) {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "agent_unauthenticated") || strings.Contains(response.Body.String(), "HoneyForge") {
				t.Fatal("telemetry host discloses application identity")
			}
		})
	}
}
