package events

import (
	"encoding/json"
	"testing"

	"honey-forge/modules/profiles"
)

func TestCustomHTTPSnapshot(t *testing.T) {
	snapshot := profiles.Snapshot{TypeID: "custom-http", TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{
		"services": []any{profiles.Object{"name": "web", "port": 8080}},
		"blocks":   []any{profiles.Object{"id": "reply", "kind": "response"}},
	}}
	for _, tc := range []struct {
		name, service, node, kind string
		port                      int
		valid                     bool
	}{
		{"matching block", "web", "reply", "response", 8080, true},
		{"wrong port", "web", "reply", "response", 8081, false},
		{"wrong service", "other", "reply", "response", 8080, false},
		{"unknown block", "web", "missing", "response", 8080, false},
		{"wrong kind", "web", "reply", "call", 8080, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"service": tc.service, "node_id": tc.node, "kind": tc.kind})
			if err != nil {
				t.Fatal(err)
			}
			event := AgentEvent{TypeID: "custom-http", TypeVersion: 1, ProfileRevision: 1, EventType: "http.block", Destination: Destination{Protocol: "tcp", Port: tc.port}, Data: raw}
			if err := checkSnapshot(event, snapshot); (err == nil) != tc.valid {
				t.Fatalf("snapshot: %v", err)
			}
		})
	}
}
