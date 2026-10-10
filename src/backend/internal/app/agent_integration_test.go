//go:build integration

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentRouteRequiresBoundaries(t *testing.T) {
	runtime := openIntegrationRuntime(t)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/assets/stream", nil)
	runtime.Router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("agent route returned %d", response.Code)
	}
}
