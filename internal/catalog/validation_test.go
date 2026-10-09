package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

const validConfig = `{"listeners":[{"name":"main","port":2222,"banner":"SSH-2.0-demo\r\n","close_after_banner":false}],"logging":{"capture_payload":true,"max_payload_bytes":4},"management":{"heartbeat_interval_seconds":5,"telemetry_flush_interval_ms":100}}`

func TestSchemaIntegerRepresentations(t *testing.T) {
	s := testService(t)
	for _, tt := range []struct{ name, raw, code string }{
		{"decimal whole port", strings.Replace(validConfig, `2222`, `2222.0`, 1), ""},
		{"exponent whole port", strings.Replace(validConfig, `2222`, `2.222e3`, 1), ""},
		{"decimal capture limit", strings.Replace(validConfig, `"max_payload_bytes":4`, `"max_payload_bytes":4.0`, 1), ""},
		{"fractional port", strings.Replace(validConfig, `2222`, `2222.5`, 1), "config_invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "tcp-banner", 1, json.RawMessage(tt.raw)), tt.code)
		})
	}
	wantCode(t, s.CheckEvent(t.Context(), "tcp-banner", 1, "tcp.payload_received", json.RawMessage(`{"listener_name":"main","payload_base64":"YQ==","captured_bytes":1.0,"original_bytes":1e0,"truncated":false}`)), "")
	wantCode(t, s.CheckActionResult(t.Context(), "tcp-banner", 1, "start", json.RawMessage(`{"runtime_state":"running","applied_profile_revision":1.0}`)), "")
}

func TestCheckConfig(t *testing.T) {
	s := testService(t)
	for _, tt := range []struct{ name, raw, code string }{
		{"success", validConfig, ""},
		{"missing section", `{"listeners":[]}`, "config_invalid"},
		{"unknown field", strings.Replace(validConfig, `"port":2222`, `"bind_address":"evil","port":2222`, 1), "config_invalid"},
		{"duplicate name", strings.Replace(validConfig, `"listeners":[`, `"listeners":[{"name":"main","port":2223,"banner":"x","close_after_banner":true},`, 1), "config_invalid"},
		{"duplicate port", strings.Replace(validConfig, `"listeners":[`, `"listeners":[{"name":"other","port":2222,"banner":"x","close_after_banner":true},`, 1), "config_invalid"},
		{"unicode byte limit", strings.Replace(validConfig, `SSH-2.0-demo\r\n`, strings.Repeat("я", 2049), 1), "config_invalid"},
		{"unicode byte boundary", strings.Replace(validConfig, `SSH-2.0-demo\r\n`, strings.Repeat("я", 2048), 1), ""},
		{"capture disabled nonzero", strings.Replace(validConfig, `"capture_payload":true`, `"capture_payload":false`, 1), "config_invalid"},
		{"capture enabled zero", strings.Replace(validConfig, `"max_payload_bytes":4`, `"max_payload_bytes":0`, 1), "config_invalid"},
		{"capture disabled zero", strings.Replace(strings.Replace(validConfig, `"capture_payload":true`, `"capture_payload":false`, 1), `"max_payload_bytes":4`, `"max_payload_bytes":0`, 1), ""},
		{"port overflow", strings.Replace(validConfig, `2222`, `65536`, 1), "config_invalid"},
		{"heartbeat overflow", strings.Replace(validConfig, `"heartbeat_interval_seconds":5`, `"heartbeat_interval_seconds":11`, 1), "config_invalid"},
		{"full config required", strings.Replace(validConfig, `,"close_after_banner":false`, "", 1), "config_invalid"},
		{"malformed", `{`, "config_invalid"},
		{"oversize", `{"x":"` + strings.Repeat("a", 131072) + `"}`, "config_too_large"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "tcp-banner", 1, json.RawMessage(tt.raw)), tt.code)
		})
	}
}

func TestCheckEvent(t *testing.T) {
	s := testService(t)
	for _, tt := range []struct{ name, event, raw, code string }{
		{"opened", "tcp.connection_opened", `{"listener_name":"main"}`, ""},
		{"closed", "tcp.connection_closed", `{"listener_name":"main","duration_ms":0,"bytes_received":0,"reason":"idle_timeout"}`, ""},
		{"closed reason", "tcp.connection_closed", `{"listener_name":"main","duration_ms":0,"bytes_received":0,"reason":"authenticated"}`, "telemetry_invalid"},
		{"payload", "tcp.payload_received", `{"listener_name":"main","payload_base64":"YWJj","captured_bytes":3,"original_bytes":3,"truncated":false}`, ""},
		{"truncated", "tcp.payload_received", `{"listener_name":"main","payload_base64":"YQ==","captured_bytes":1,"original_bytes":3,"truncated":true}`, ""},
		{"empty capture", "tcp.payload_received", `{"listener_name":"main","payload_base64":"","captured_bytes":0,"original_bytes":3,"truncated":true}`, ""},
		{"invalid base64", "tcp.payload_received", `{"listener_name":"main","payload_base64":"!!!!","captured_bytes":3,"original_bytes":3,"truncated":false}`, "telemetry_invalid"},
		{"length mismatch", "tcp.payload_received", `{"listener_name":"main","payload_base64":"YQ==","captured_bytes":2,"original_bytes":3,"truncated":true}`, "telemetry_invalid"},
		{"truncated mismatch", "tcp.payload_received", `{"listener_name":"main","payload_base64":"YQ==","captured_bytes":1,"original_bytes":3,"truncated":false}`, "telemetry_invalid"},
		{"undeclared auth", "service.auth_attempt", `{}`, "telemetry_invalid"},
		{"undeclared action", "service.action", `{}`, "telemetry_invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantCode(t, s.CheckEvent(t.Context(), "tcp-banner", 1, tt.event, json.RawMessage(tt.raw)), tt.code)
		})
	}
}

func TestCheckAction(t *testing.T) {
	s := testService(t)
	for _, tt := range []struct{ name, action, params, result, code string }{
		{"start", "start", `{}`, `{"runtime_state":"running","applied_profile_revision":1}`, ""},
		{"stop before config", "stop", `{}`, `{"runtime_state":"stopped","applied_profile_revision":null}`, ""},
		{"apply", "apply_config", `{"profile_revision":2147483647}`, `{"runtime_state":"stopped","applied_profile_revision":2147483647}`, ""},
		{"unsupported", "exec", `{}`, `{}`, "unsupported_action"},
		{"nonempty start", "start", `{"shell":"bash"}`, `{}`, "command_params_invalid"},
		{"missing revision", "apply_config", `{}`, `{}`, "command_params_invalid"},
		{"overflow revision", "apply_config", `{"profile_revision":2147483648}`, `{}`, "command_params_invalid"},
		{"null start result", "start", `{}`, `{"runtime_state":"running","applied_profile_revision":null}`, "command_result_invalid"},
		{"null apply result", "apply_config", `{"profile_revision":1}`, `{"runtime_state":"stopped","applied_profile_revision":null}`, "command_result_invalid"},
		{"null running stop", "stop", `{}`, `{"runtime_state":"running","applied_profile_revision":null}`, "command_result_invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.CheckAction(t.Context(), "tcp-banner", 1, tt.action, json.RawMessage(tt.params))
			if err == nil {
				err = s.CheckActionResult(t.Context(), "tcp-banner", 1, tt.action, json.RawMessage(tt.result))
			}
			wantCode(t, err, tt.code)
		})
	}
}

func TestCheckPayloadCapture(t *testing.T) {
	s := testService(t)
	payload := json.RawMessage(`{"listener_name":"main","payload_base64":"YWJj","captured_bytes":3,"original_bytes":5,"truncated":true}`)
	next, err := s.CheckPayloadCapture(t.Context(), "tcp-banner", 1, json.RawMessage(validConfig), payload, 0)
	wantCode(t, err, "")
	if next != 3 {
		t.Fatal(next)
	}
	_, err = s.CheckPayloadCapture(t.Context(), "tcp-banner", 1, json.RawMessage(validConfig), payload, 3)
	wantCode(t, err, "telemetry_invalid")
	disabled := strings.Replace(strings.Replace(validConfig, `"capture_payload":true`, `"capture_payload":false`, 1), `"max_payload_bytes":4`, `"max_payload_bytes":0`, 1)
	_, err = s.CheckPayloadCapture(t.Context(), "tcp-banner", 1, json.RawMessage(disabled), payload, 0)
	wantCode(t, err, "telemetry_invalid")
}

func TestTCPBoundaries(t *testing.T) {
	s := testService(t)
	for _, tt := range []struct{ name, from, to, code string }{
		{"minimum port", "2222", "1", ""},
		{"maximum port", "2222", "65535", ""},
		{"zero port", "2222", "0", "config_invalid"},
		{"negative port", "2222", "-1", "config_invalid"},
		{"invalid name", `"name":"main"`, `"name":"Upper"`, "config_invalid"},
		{"empty banner", `SSH-2.0-demo\r\n`, "", "config_invalid"},
		{"maximum capture", `"max_payload_bytes":4`, `"max_payload_bytes":4096`, ""},
		{"capture overflow", `"max_payload_bytes":4`, `"max_payload_bytes":4097`, "config_invalid"},
		{"heartbeat maximum", `"heartbeat_interval_seconds":5`, `"heartbeat_interval_seconds":10`, ""},
		{"heartbeat minimum", `"heartbeat_interval_seconds":5`, `"heartbeat_interval_seconds":4`, "config_invalid"},
		{"flush maximum", `"telemetry_flush_interval_ms":100`, `"telemetry_flush_interval_ms":1000`, ""},
		{"flush overflow", `"telemetry_flush_interval_ms":100`, `"telemetry_flush_interval_ms":1001`, "config_invalid"},
		{"duplicate key", `"port":2222`, `"port":1,"port":2222`, "config_invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := strings.Replace(validConfig, tt.from, tt.to, 1)
			wantCode(t, s.CheckConfig(t.Context(), "tcp-banner", 1, json.RawMessage(raw)), tt.code)
		})
	}
	for _, count := range []int{0, 1, 16, 17} {
		t.Run("listener count "+string(rune('A'+count)), func(t *testing.T) {
			var config map[string]any
			if err := json.Unmarshal([]byte(validConfig), &config); err != nil {
				t.Fatal(err)
			}
			listeners := []any{}
			for i := 0; i < count; i++ {
				listeners = append(listeners, map[string]any{"name": "listener" + string(rune('a'+i)), "port": 10000 + i, "banner": "demo", "close_after_banner": false})
			}
			config["listeners"] = listeners
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			code := ""
			if count < 1 || count > 16 {
				code = "config_invalid"
			}
			wantCode(t, s.CheckConfig(t.Context(), "tcp-banner", 1, raw), code)
		})
	}
}
