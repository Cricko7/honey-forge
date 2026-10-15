package events

import (
	"encoding/json"
	"testing"

	"honey-forge/modules/profiles"
)

func TestConpotSnapshotTransportBoundary(t *testing.T) {
	s := profiles.Snapshot{TypeID: "conpot", TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{"services": []any{map[string]any{"name": "plc", "port": 161, "protocol": "snmp"}}}}
	for _, tc := range []struct {
		name, transport, service string
		port                     int
		valid                    bool
	}{
		{"UDP SNMP", "udp", "plc", 161, true},
		{"wrong transport", "tcp", "plc", 161, false},
		{"other service", "udp", "other", 161, false},
		{"other port", "udp", "plc", 162, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(map[string]string{"service": tc.service})
			if err != nil {
				t.Fatal(err)
			}
			e := AgentEvent{TypeID: "conpot", TypeVersion: 1, ProfileRevision: 1, EventType: "service.action", Destination: Destination{Protocol: tc.transport, Port: tc.port}, Data: data}
			if err := checkSnapshot(e, s); (err == nil) != tc.valid {
				t.Fatalf("snapshot check: %v", err)
			}
		})
	}
}
