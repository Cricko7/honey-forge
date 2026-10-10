package events

import (
	"encoding/json"
	"honey-forge/modules/profiles"
	"strings"
	"testing"
	"time"
)

func TestEventEnvelope(t *testing.T) {
	valid := `{"event_id":"11111111-1111-4111-8111-111111111111","event_type":"tcp.connection_opened","type_id":"tcp-banner","type_version":1,"profile_revision":1,"occurred_at":"2026-10-10T12:00:00Z","session_id":"22222222-2222-4222-8222-222222222222","session_sequence":1,"source":{"ip":"2001:0db8::1","port":50000},"destination":{"protocol":"tcp","port":2222},"data":{"listener_name":"ssh"}}`
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, body string
		valid      bool
	}{
		{"valid", valid, true},
		{"hostname", strings.Replace(valid, "2001:0db8::1", "attacker.example", 1), false},
		{"IP zone", strings.Replace(valid, "2001:0db8::1", "fe80::1%eth0", 1), false},
		{"zero sequence", strings.Replace(valid, `"session_sequence":1`, `"session_sequence":0`, 1), false},
		{"future", strings.Replace(valid, "12:00:00Z", "12:06:00Z", 1), false},
		{"five minute boundary", strings.Replace(valid, "12:00:00Z", "12:05:00Z", 1), true},
		{"offline history", strings.Replace(valid, "2026-10-10T12:00:00Z", "1999-01-01T00:00:00Z", 1), true},
		{"oversized JSON", valid + strings.Repeat(" ", 16*1024), false},
		{"duplicate data key", strings.Replace(valid, `{"listener_name":"ssh"}`, `{"listener_name":"ssh","listener_name":"http"}`, 1), false},
		{"unknown source field", strings.Replace(valid, `"port":50000}`, `"port":50000,"hostname":"evil"}`, 1), false},
		{"missing source", strings.Replace(valid, `"source":{"ip":"2001:0db8::1","port":50000},`, "", 1), false},
		{"sequence overflow", strings.Replace(valid, `"session_sequence":1`, `"session_sequence":9007199254740992`, 1), false},
		{"null data", strings.Replace(valid, `{"listener_name":"ssh"}`, `null`, 1), false},
		{"unknown field", strings.TrimSuffix(valid, "}") + `,"token":"secret"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			event, err := parseEvent(json.RawMessage(tt.body), now)
			if (err == nil) != tt.valid {
				t.Fatalf("err=%v", err)
			}
			if tt.valid && event.Source.IP != "2001:db8::1" {
				t.Fatal("IP not canonical")
			}
		})
	}
}

func TestStructuredSnapshot(t *testing.T) {
	snapshot := profiles.Snapshot{TypeID: "service-demo", TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{"services": []any{profiles.Object{"name": "ssh"}}}}
	for _, tc := range []struct {
		name, service string
		valid         bool
	}{
		{"declared", "ssh", true}, {"foreign service", "http", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := AgentEvent{TypeID: "service-demo", TypeVersion: 1, ProfileRevision: 1, EventType: "service.action", Data: json.RawMessage(`{"service":"` + tc.service + `"}`)}
			if err := checkSnapshot(e, snapshot); (err == nil) != tc.valid {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
