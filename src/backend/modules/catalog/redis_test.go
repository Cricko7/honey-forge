package catalog

import (
	"encoding/json"
	"strings"
	"testing"

	"honey-forge/internal/contract"
)

func TestRedisBuiltinContract(t *testing.T) {
	defs := BuiltinDefinitions()
	if len(defs) != 2 || defs[1].Entry.TypeID != "redis-emulator" || defs[1].Entry.InteractionLevel != "medium" || !defs[1].SupportsAuthentication || !defs[1].SupportsServiceActions {
		t.Fatalf("Redis descriptor missing: %+v", defs)
	}
	s := testService(t, defs...)
	config := `{"services":[{"name":"redis","port":6380,"password":"bait-pass"}],"management":{"heartbeat_interval_seconds":5,"telemetry_flush_interval_ms":100}}`
	for _, tc := range []struct{ name, raw, code string }{
		{"valid", config, ""},
		{"no password", strings.Replace(config, `,"password":"bait-pass"`, "", 1), "config_invalid"},
		{"second service", strings.Replace(config, `}],"management"`, `},{"name":"other","port":6381,"password":"x"}],"management"`, 1), "config_invalid"},
		{"bad port", strings.Replace(config, `"port":6380`, `"port":0`, 1), "config_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "redis-emulator", 1, json.RawMessage(tc.raw)), tc.code)
		})
	}
	entry, err := s.LookupType(t.Context(), "redis-emulator", contract.TypeVersion(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"service.connection_opened", "service.auth_attempt", "service.action", "service.connection_closed"} {
		found := false
		for _, event := range entry.EventSchemas {
			if string(event.EventType) == kind {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s", kind)
		}
	}
	if err := s.CheckEvent(t.Context(), "redis-emulator", 1, "service.action", json.RawMessage(`{"service":"redis","action_kind":"command","input":"GET demo","outcome":"accepted","truncated":false,"received_bytes":10}`)); err != nil {
		t.Fatal(err)
	}
	configSchema, err := s.ConfigSchema(t.Context(), "redis-emulator", 1)
	if err != nil {
		t.Fatal(err)
	}
	public, paths, err := configSchema.PublicConfig(json.RawMessage(config))
	if err != nil || strings.Contains(string(public), "bait-pass") || len(paths) != 1 || paths[0] != "/services/0/password" {
		t.Fatalf("password leaked or not marked secret: %s %v %v", public, paths, err)
	}
}
