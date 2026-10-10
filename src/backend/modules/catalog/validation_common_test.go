package catalog

import (
	"encoding/json"
	"strings"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
)

// Check the actual catalog -> module 01 boundary rather than duplicating schema
// or secret handling in a fake implementation.
func TestCommonSecretMergeCompatibility(t *testing.T) {
	definition := BuiltinDefinitions()[0]
	definition.Entry.TypeID = "secret-demo"
	definition.Entry.ConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["name","password"],"properties":{"name":{"type":"string","minLength":1},"password":{"type":"string","minLength":1,"writeOnly":true}}}`)
	s := testService(t, definition)
	schema, err := s.ConfigSchema(t.Context(), "secret-demo", 1)
	wantCode(t, err, "")
	previous := json.RawMessage(`{"name":"old","password":"server-secret"}`)
	effective, err := schema.MergeConfig(previous, json.RawMessage(`{"name":"new"}`), nil)
	wantCode(t, err, "")
	wantCode(t, s.CheckConfig(t.Context(), "secret-demo", 1, effective), "")
	public, fields, err := schema.PublicConfig(effective)
	wantCode(t, err, "")
	if strings.Contains(string(public), "server-secret") || len(fields) != 1 || fields[0] != "/password" {
		t.Fatalf("secret boundary: %s %v", public, fields)
	}
	_, err = schema.MergeConfig(previous, json.RawMessage(`{"name":"new","password":"replacement"}`), []string{"/password"})
	wantCode(t, err, "validation_failed")
	if !strings.Contains(string(previous), "server-secret") {
		t.Fatal("common merge mutated stored input")
	}
}

func TestCommonFingerprintCompatibility(t *testing.T) {
	s := testService(t)
	decimal := json.RawMessage(strings.Replace(validConfig, `2222`, `2222.0`, 1))
	wantCode(t, s.CheckConfig(t.Context(), "tcp-banner", contract.TypeVersion(1), decimal), "")
	a, err := mutation.Fingerprint(json.RawMessage(validConfig))
	wantCode(t, err, "")
	b, err := mutation.Fingerprint(decimal)
	wantCode(t, err, "")
	if a != b {
		t.Fatal("catalog-equivalent config has different common idempotency fingerprint")
	}
}
