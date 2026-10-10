package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

const sshConfig = `{"services":[{"name":"ssh","port":2222,"username":"root","password":"bait-password"}],"session":{"max_seconds":300,"max_connections":4,"max_recording_bytes":1048576,"max_upload_bytes":1048576},"management":{"heartbeat_interval_seconds":5,"telemetry_flush_interval_ms":100}}`

func TestSSHConfigurationAndRecording(t *testing.T) {
	s := testService(t)
	for _, tc := range []struct{ name, raw, code string }{
		{"valid", sshConfig, ""},
		{"unknown field", strings.Replace(sshConfig, `"max_seconds":300`, `"max_seconds":300,"image":"attacker"`, 1), "config_invalid"},
		{"invalid port", strings.Replace(sshConfig, `"port":2222`, `"port":0`, 1), "config_invalid"},
		{"unbounded session", strings.Replace(sshConfig, `"max_seconds":300`, `"max_seconds":0`, 1), "config_invalid"},
		{"too many connections", strings.Replace(sshConfig, `"max_connections":4`, `"max_connections":1000`, 1), "config_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "ssh-high-interaction", 1, json.RawMessage(tc.raw)), tc.code)
		})
	}
	schema, err := s.ConfigSchema(t.Context(), "ssh-high-interaction", 1)
	if err != nil {
		t.Fatal(err)
	}
	public, paths, err := schema.PublicConfig(json.RawMessage(sshConfig))
	if err != nil || strings.Contains(string(public), "bait-password") || len(paths) != 1 {
		t.Fatalf("secret leaked: %s %v %v", public, paths, err)
	}
	wantCode(t, s.CheckEvent(t.Context(), "ssh-high-interaction", 1, "service.session_io", json.RawMessage(`{"service":"ssh","channel_id":"11111111-1111-4111-8111-111111111111","stream":"stdin","payload_base64":"aGk=","bytes":2}`)), "")
	wantCode(t, s.CheckEvent(t.Context(), "ssh-high-interaction", 1, "service.session_io", json.RawMessage(`{"service":"ssh","channel_id":"11111111-1111-4111-8111-111111111111","stream":"stdin","payload_base64":"@@","bytes":2}`)), "telemetry_invalid")
	wantCode(t, s.CheckEvent(t.Context(), "ssh-high-interaction", 1, "service.session_io", json.RawMessage(`{"service":"ssh","channel_id":"11111111-1111-4111-8111-111111111111","stream":"stdin","payload_base64":"aGk=","bytes":3}`)), "telemetry_invalid")
}
