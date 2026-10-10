package catalog

import (
	"encoding/json"
	"honey-forge/internal/contract"
	"strings"
	"testing"
)

func TestStructuredEvents(t *testing.T) {
	defs := BuiltinDefinitions()
	defs[0].Entry.TypeID = "service-demo"
	defs[0].Entry.EventSchemas = StructuredEventSchemas()
	codec, err := contract.NewCursorCodec([]byte(strings.Repeat("x", 32)))
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewService(defs, codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, event, data string
		valid             bool
	}{
		{"original credentials", "service.auth_attempt", `{"service":"ssh","username":" Root ","password":" <script>secret</script> ","outcome":"rejected","truncated":false}`, true},
		{"null credentials", "service.auth_attempt", `{"service":"ssh","username":null,"password":"","outcome":"unknown","truncated":false}`, true},
		{"missing password", "service.auth_attempt", `{"service":"ssh","username":"root","outcome":"rejected","truncated":false}`, false},
		{"byte boundary", "service.auth_attempt", `{"service":"ssh","username":null,"password":"` + strings.Repeat("я", 512) + `","outcome":"rejected","truncated":false}`, true},
		{"too many bytes", "service.auth_attempt", `{"service":"ssh","username":null,"password":"` + strings.Repeat("я", 513) + `","outcome":"rejected","truncated":true}`, false},
		{"original command", "service.action", `{"service":"ssh","action_kind":"command","input":" <script>alert(1)</script> ","outcome":"accepted","truncated":false}`, true},
		{"empty input", "service.action", `{"service":"ssh","action_kind":"request","input":"","outcome":"unknown","truncated":false}`, true},
		{"input byte limit", "service.action", `{"service":"ssh","action_kind":"command","input":"` + strings.Repeat("я", 2049) + `","outcome":"accepted","truncated":true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := s.CheckEvent(t.Context(), "service-demo", contract.TypeVersion(1), tc.event, json.RawMessage(tc.data))
			if (err == nil) != tc.valid {
				t.Fatalf("err=%v", err)
			}
		})
	}
	tcp, err := NewService(BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	if err := tcp.CheckEvent(t.Context(), "tcp-banner", 1, "service.auth_attempt", json.RawMessage(`{}`)); err == nil {
		t.Fatal("TCP must not accept auth events")
	}
}
