package honeytrap

import (
	"context"
	"net/http/httptest"
	"testing"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestEmbeddedHandler(t *testing.T) {
	snapshot := profiles.Snapshot{TypeID: "honeytoken-http", TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{
		"services":   []any{map[string]any{"name": "bait", "port": 32080}},
		"tokens":     []any{map[string]any{"id": "secret", "kind": "key", "path": "/secret", "value": "bait-secret-0123456789"}},
		"management": map[string]any{"heartbeat_interval_seconds": 10, "telemetry_flush_interval_ms": 500},
	}}
	for _, tc := range []struct {
		name, authorization string
		status, hits        int
	}{
		{"unauthorized", "", 401, 0}, {"valid key", "Bearer bait-secret-0123456789", 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			handler, err := NewEmbeddedHandler(t.Context(), snapshot, func(_ context.Context, e events.AgentEvent) error {
				if e.EventType == "honeytoken.triggered" {
					hits++
				}

				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest("GET", "/secret", nil)
			req.RemoteAddr = "192.0.2.1:12345"
			req.Header.Set("Authorization", tc.authorization)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status || hits != tc.hits {
				t.Fatalf("status=%d hits=%d", response.Code, hits)
			}
		})
	}
}
