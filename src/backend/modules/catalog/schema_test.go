package catalog

import (
	"encoding/json"
	"honey-forge/internal/contract"
	"strings"
	"testing"
)

func TestDynamicSchemas(t *testing.T) {
	def := BuiltinDefinitions()[0]
	def.Entry.TypeID = "dynamic-demo"
	def.Entry.InteractionLevel = "medium"
	def.SupportsAuthentication = true
	def.SupportsServiceActions = true
	def.Entry.ConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["items","choice","uuid","ip","time","email"],"$defs":{"item":{"type":"object","additionalProperties":false,"required":["value"],"properties":{"value":{"type":"integer","minimum":1}}}},"properties":{"items":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/item"}},"choice":{"oneOf":[{"type":"string","enum":["one"]},{"type":"integer","minimum":1}]},"uuid":{"type":"string","format":"uuid"},"ip":{"type":"string","format":"ipv4"},"time":{"type":"string","format":"date-time"},"email":{"type":"string","format":"email"}}}`)
	schema := json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["value"],"properties":{"value":{"type":"integer","minimum":1}}}`)
	for _, event := range []string{"service.auth_attempt", "service.action"} {
		def.Entry.EventSchemas = append(def.Entry.EventSchemas, EventDescriptor{EventType: contract.EventType(event), Title: event, DataSchema: schema})
	}
	def.Entry.Actions = append(def.Entry.Actions, ActionDescriptor{Action: "inspect", Title: "Inspect", ParamsSchema: schema, ResultSchema: schema})
	s := testService(t, def)
	good := `{"items":[{"value":1}],"choice":"one","uuid":"11111111-1111-4111-8111-111111111111","ip":"127.0.0.1","time":"2026-10-09T12:00:00Z","email":"test@example.com"}`
	for _, tt := range []struct{ name, raw, code string }{
		{"nested defs and oneOf", good, ""},
		{"uuid", strings.Replace(good, "11111111-1111-4111-8111-111111111111", "bad", 1), "config_invalid"},
		{"ip", strings.Replace(good, "127.0.0.1", "999.0.0.1", 1), "config_invalid"},
		{"time", strings.Replace(good, "2026-10-09T12:00:00Z", "2026-10-09", 1), "config_invalid"},
		{"email", strings.Replace(good, "test@example.com", "invalid", 1), "config_invalid"},
		{"nested constraint", strings.Replace(good, `"value":1`, `"value":0`, 1), "config_invalid"},
		{"enum", strings.Replace(good, `"choice":"one"`, `"choice":"two"`, 1), "config_invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "dynamic-demo", 1, json.RawMessage(tt.raw)), tt.code)
		})
	}
	_, err := s.CheckAction(t.Context(), "dynamic-demo", 1, "inspect", json.RawMessage(`{"value":1}`))
	wantCode(t, err, "")
	wantCode(t, s.CheckActionResult(t.Context(), "dynamic-demo", 1, "inspect", json.RawMessage(`{"value":0}`)), "command_result_invalid")
	wantCode(t, s.CheckEvent(t.Context(), "dynamic-demo", 1, "service.auth_attempt", json.RawMessage(`{"value":1}`)), "")
	wantCode(t, s.CheckEvent(t.Context(), "dynamic-demo", 1, "service.auth_attempt", json.RawMessage(`{"value":0}`)), "telemetry_invalid")
}

func TestSchemaCopies(t *testing.T) {
	defs := BuiltinDefinitions()
	s := testService(t, defs...)
	defs[0].Entry.ConfigSchema[0] = '!'
	defs[0].Entry.UI.FieldOrder[0] = "changed"
	wantCode(t, s.CheckConfig(t.Context(), "tcp-banner", 1, json.RawMessage(validConfig)), "")
	descriptor, err := s.CheckAction(t.Context(), "tcp-banner", 1, "start", json.RawMessage(`{}`))
	wantCode(t, err, "")
	descriptor.ParamsSchema[0] = '!'
	_, err = s.CheckAction(t.Context(), "tcp-banner", 1, "start", json.RawMessage(`{}`))
	wantCode(t, err, "")
	schema, err := s.ConfigSchema(t.Context(), "tcp-banner", 1)
	wantCode(t, err, "")
	wantCode(t, schema.Validate(json.RawMessage(validConfig), "/config"), "")
}

func TestConfiguredRuntimeResult(t *testing.T) {
	s := testService(t)
	result := json.RawMessage(`{"runtime_state":"stopped","applied_profile_revision":null}`)
	wantCode(t, s.CheckRuntimeResult(t.Context(), "tcp-banner", 1, "stop", result, false), "")
	wantCode(t, s.CheckRuntimeResult(t.Context(), "tcp-banner", 1, "stop", result, true), "command_result_invalid")
}

func TestRecursiveSchemasAndDefaults(t *testing.T) {
	definition := BuiltinDefinitions()[0]
	definition.Entry.TypeID = "recursive-demo"
	definition.Entry.ConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["root","ip"],"properties":{"root":{"$ref":"#/$defs/node"},"ip":{"type":"string","format":"ipv6"}},"$defs":{"node":{"type":"object","additionalProperties":false,"required":["label"],"properties":{"label":{"type":"string","minLength":1,"default":"hint"},"children":{"type":"array","items":{"$ref":"#/$defs/node"}}}}}}`)
	s := testService(t, definition)
	for _, tt := range []struct{ name, raw, code string }{
		{"recursive objects and arrays", `{"root":{"label":"parent","children":[{"label":"child","children":[{"label":"leaf"}]}]},"ip":"::1"}`, ""},
		{"missing value is not defaulted", `{"root":{},"ip":"::1"}`, "config_invalid"},
		{"invalid nested value", `{"root":{"label":"parent","children":[{"label":""}]},"ip":"::1"}`, "config_invalid"},
		{"invalid ipv6", `{"root":{"label":"parent"},"ip":"not-an-ip"}`, "config_invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.raw)
			before := string(raw)
			wantCode(t, s.CheckConfig(t.Context(), "recursive-demo", 1, raw), tt.code)
			if string(raw) != before {
				t.Fatal("validation changed request")
			}
		})
	}
}
