package events

import (
	"encoding/json"
	"testing"

	"honey-forge/modules/profiles"
)

func TestHoneytokenSnapshot(t *testing.T) {
	snapshot := profiles.Snapshot{TypeID: "honeytoken-http", TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{
		"services": []any{map[string]any{"name": "web", "port": 8080}},
		"tokens":   []any{map[string]any{"id": "backup", "kind": "file", "path": "/backup.txt", "value": "fake"}},
	}}
	for _, tc := range []struct {
		name, id, kind string
		port           int
		valid          bool
	}{
		{"matching token", "backup", "file", 8080, true},
		{"unknown token", "other", "file", 8080, false},
		{"wrong kind", "backup", "key", 8080, false},
		{"wrong port", "backup", "file", 8081, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"service": "web", "token_id": tc.id, "kind": tc.kind, "method": "GET"})
			if err != nil {
				t.Fatal(err)
			}
			e := AgentEvent{TypeID: snapshot.TypeID, TypeVersion: 1, ProfileRevision: 1, EventType: "honeytoken.triggered", Destination: Destination{Protocol: "tcp", Port: tc.port}, Data: raw}
			if err := checkSnapshot(e, snapshot); (err == nil) != tc.valid {
				t.Fatalf("checkSnapshot: %v", err)
			}
		})
	}
}
