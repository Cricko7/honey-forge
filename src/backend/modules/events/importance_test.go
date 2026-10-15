package events

import (
	"encoding/json"
	"testing"
)

func TestSummaryImportance(t *testing.T) {
	for _, tc := range []struct {
		name, kind, data string
		severity         string
	}{
		{"connection", "tcp.connection_opened", `{}`, "low"},
		{"authentication", "service.auth_attempt", `{}`, "high"},
		{"ssh authentication", "ssh.auth_attempt", `{}`, "high"},
		{"command", "service.action", `{}`, "high"},
		{"honeytoken", "honeytoken.triggered", `{}`, "high"},
		{"custom high", "http.custom", `{"severity":"high","payload":{"password":"secret"}}`, "high"},
		{"custom critical", "http.custom", `{"severity":"critical"}`, "critical"},
		{"custom medium", "http.custom", `{"severity":"medium"}`, "medium"},
		{"custom low", "http.custom", `{"severity":"low"}`, "low"},
		{"custom missing", "http.custom", `{}`, "low"},
		{"custom malformed", "http.custom", `invalid`, "low"},
		{"unrelated severity", "http.block", `{"severity":"critical"}`, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Event{AgentEvent: AgentEvent{EventType: tc.kind, Data: json.RawMessage(tc.data)}}
			raw, err := json.Marshal(e.Summary())
			if err != nil {
				t.Fatal(err)
			}

			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got["severity"] != tc.severity {
				t.Fatalf("severity = %v, want %v", got["severity"], tc.severity)
			}
			if _, exists := got["data"]; exists {
				t.Fatal("summary exposes event data")
			}
		})
	}
}
