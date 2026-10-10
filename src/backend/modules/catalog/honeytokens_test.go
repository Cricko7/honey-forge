package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHoneytokenConfiguration(t *testing.T) {
	s := testService(t)
	config := `{"services":[{"name":"web","port":8080}],"tokens":[{"id":"backup","kind":"key","path":"/v1/backups","value":"fake-key-0123456789"}],"management":{"heartbeat_interval_seconds":5,"telemetry_flush_interval_ms":100}}`
	for _, tc := range []struct{ name, raw, code string }{
		{"valid", config, ""},
		{"invalid kind", strings.Replace(config, `"key"`, `"password"`, 1), "config_invalid"},
		{"invalid path", strings.Replace(config, `/v1/backups`, `/../backups`, 1), "config_invalid"},
		{"empty value", strings.Replace(config, `fake-key-0123456789`, ``, 1), "config_invalid"},
		{"short key", strings.Replace(config, `fake-key-0123456789`, `short`, 1), "config_invalid"},
		{"key whitespace", strings.Replace(config, `fake-key-0123456789`, `fake key-0123456789`, 1), "config_invalid"},
		{"reserved path", strings.Replace(config, `/v1/backups`, `/artifacts/backup`, 1), "config_invalid"},
		{"unclean path", strings.Replace(config, `/v1/backups`, `/v1//backups`, 1), "config_invalid"},
		{"invalid port", strings.Replace(config, `"port":8080`, `"port":0`, 1), "config_invalid"},
		{"duplicate token", strings.Replace(config, `}],"management"`, `},{"id":"backup","kind":"url","path":"/other","value":"ok"}],"management"`, 1), "config_invalid"},
		{"duplicate path", strings.Replace(config, `}],"management"`, `},{"id":"other","kind":"url","path":"/v1/backups","value":"ok"}],"management"`, 1), "config_invalid"},
		{"duplicate key", strings.Replace(config, `}],"management"`, `},{"id":"other","kind":"key","path":"/other","value":"fake-key-0123456789"}],"management"`, 1), "config_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "honeytoken-http", 1, json.RawMessage(tc.raw)), tc.code)
		})
	}
	schema, err := s.ConfigSchema(t.Context(), "honeytoken-http", 1)
	if err != nil {
		t.Fatal(err)
	}
	public, paths, err := schema.PublicConfig(json.RawMessage(config))
	if err != nil || strings.Contains(string(public), "fake-key") || len(paths) != 1 {
		t.Fatalf("unsafe public config: %s %v %v", public, paths, err)
	}
	wantCode(t, s.CheckEvent(t.Context(), "honeytoken-http", 1, "honeytoken.triggered", json.RawMessage(`{"service":"web","token_id":"backup","kind":"key","method":"GET"}`)), "")
}
