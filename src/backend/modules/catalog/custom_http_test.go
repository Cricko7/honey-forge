package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCustomHTTPConfigReferences(t *testing.T) {
	service := testService(t)
	raw := `{"services":[{"name":"web","port":8080,"entry":"web"}],"blocks":[{"id":"web","kind":"input","config":{"name":"web","port":8080,"method":"ANY","path":"*"},"outputs":{"out":"reply"}},{"id":"reply","kind":"response","config":{"status":200,"format":"json","body":"{}","headers":"{}"},"outputs":{}}],"limits":{"max_steps":512,"timeout_ms":10000,"max_body_bytes":65536},"management":{"heartbeat_interval_seconds":10,"telemetry_flush_interval_ms":500}}`
	for _, tc := range []struct{ name, raw, code string }{
		{"valid", raw, ""},
		{"missing entry", strings.Replace(raw, `"entry":"web"`, `"entry":"missing"`, 1), "config_invalid"},
		{"missing target", strings.Replace(raw, `"out":"reply"`, `"out":"missing"`, 1), "config_invalid"},
		{"wrong service port", strings.Replace(raw, `"port":8080`, `"port":8081`, 1), "config_invalid"},
		{"unknown kind", strings.Replace(raw, `"kind":"response"`, `"kind":"exec"`, 1), "config_invalid"},
		{"missing output", strings.Replace(raw, `"out":"reply"`, ``, 1), "config_invalid"},
		{"invalid JSON body", strings.Replace(raw, `"body":"{}"`, `"body":"{"`, 1), "config_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, service.CheckConfig(t.Context(), "custom-http", 1, json.RawMessage(tc.raw)), tc.code)
		})
	}
}
