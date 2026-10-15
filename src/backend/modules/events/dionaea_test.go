package events

import (
	"encoding/json"
	"testing"

	"honey-forge/modules/profiles"
)

func TestDionaeaSnapshotTransportBoundary(t *testing.T) {
	s := profiles.Snapshot{TypeID: "dionaea", TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{"services": []any{map[string]any{"name": "plc", "port": 1445, "protocol": "smb"}}}}
	for _, tc := range []struct {
		name, transport, service string
		port                     int
		valid                    bool
	}{
		{"TCP SMB", "tcp", "plc", 1445, true},
		{"wrong transport", "udp", "plc", 1445, false},
		{"other service", "tcp", "other", 1445, false},
		{"other port", "tcp", "plc", 1446, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(map[string]string{"service": tc.service})
			if err != nil {
				t.Fatal(err)
			}
			e := AgentEvent{TypeID: "dionaea", TypeVersion: 1, ProfileRevision: 1, EventType: "service.action", Destination: Destination{Protocol: tc.transport, Port: tc.port}, Data: data}
			if err := checkSnapshot(e, s); (err == nil) != tc.valid {
				t.Fatalf("snapshot check: %v", err)
			}
		})
	}
}
