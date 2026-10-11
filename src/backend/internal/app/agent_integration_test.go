//go:build integration

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentRouteServesResourceWithoutCredentials(t *testing.T) {
	runtime := openIntegrationRuntime(t)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/assets/stream", nil)
	request.Host = "resources.example"
	runtime.Router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/javascript; charset=utf-8" {
		t.Fatalf("agent route returned %d", response.Code)
	}
}
