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

func TestCustomHTTPEventBlock(t *testing.T) {
	service := testService(t)
	raw := `{"services":[{"name":"web","port":8080,"entry":"web"}],"blocks":[{"id":"web","kind":"input","config":{"name":"web","port":8080,"method":"ANY","path":"*"},"outputs":{"out":"notify"}},{"id":"notify","kind":"event","config":{"name":"pipeline.stage","severity":"high","payload":"{\"user\":\"${json.user}\"}"},"outputs":{"out":"reply"}},{"id":"reply","kind":"response","config":{"status":200,"format":"json","body":"{}","headers":"{}"},"outputs":{}}],"limits":{"max_steps":512,"timeout_ms":10000,"max_body_bytes":65536},"management":{"heartbeat_interval_seconds":10,"telemetry_flush_interval_ms":500}}`
	for _, tc := range []struct{ name, raw, code string }{
		{"valid", raw, ""},
		{"invalid name", strings.Replace(raw, `"name":"pipeline.stage"`, `"name":"${json.name}"`, 1), "config_invalid"},
		{"invalid severity", strings.Replace(raw, `"severity":"high"`, `"severity":"urgent"`, 1), "config_invalid"},
		{"invalid payload JSON", strings.Replace(raw, `"payload":"{\"user\":\"${json.user}\"}"`, `"payload":"{"`, 1), "config_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, service.CheckConfig(t.Context(), "custom-http", 1, json.RawMessage(tc.raw)), tc.code)
		})
	}
	for _, tc := range []struct{ name, data, code string }{
		{"typed data", `{"service":"web","node_id":"notify","name":"pipeline.stage","severity":"high","payload":{"count":3,"valid":true}}`, ""},
		{"invalid event name", `{"service":"web","node_id":"notify","name":"${json.name}","severity":"high","payload":{}}`, "telemetry_invalid"},
		{"oversized payload", `{"service":"web","node_id":"notify","name":"stage","severity":"high","payload":"` + strings.Repeat("x", 4096) + `"}`, "telemetry_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, service.CheckEvent(t.Context(), "custom-http", 1, "http.custom", json.RawMessage(tc.data)), tc.code)
		})
	}
}
