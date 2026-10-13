package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

const processData = `{"service":"ssh","schema_version":1,"sensor":"tetragon","sensor_version":"1.7.0","host_id":"node-a","generation":1,"container_id":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `","container_started_at":"2026-10-13T12:00:00Z","pid":2345,"ppid":1,"uid":1000,"binary":"/usr/bin/id","cwd":"/home/sandbox","arguments_redacted":true}`

func TestHostTelemetryCatalogVersion(t *testing.T) {
	s := testService(t)
	wantCode(t, s.CheckConfig(t.Context(), "ssh-high-interaction", 2, json.RawMessage(sshConfig)), "")
	wantCode(t, s.CheckEvent(t.Context(), "ssh-high-interaction", 1, "process.exec", json.RawMessage(processData)), "telemetry_invalid")
	for _, tc := range []struct{ name, data, code string }{
		{"valid", processData, ""},
		{"unknown secret", strings.Replace(processData, `"pid":2345`, `"pid":2345,"arguments":"secret"`, 1), "telemetry_invalid"},
		{"invalid container", strings.Replace(processData, strings.Repeat("a", 64), "1234", 1), "telemetry_invalid"},
		{"invalid start", strings.Replace(processData, "2026-10-13T12:00:00Z", "not-time", 1), "telemetry_invalid"},
		{"invalid generation", strings.Replace(processData, `"generation":1`, `"generation":0`, 1), "telemetry_invalid"},
		{"pid missing", strings.Replace(processData, `"pid":2345,`, "", 1), "telemetry_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, s.CheckEvent(t.Context(), "ssh-high-interaction", 2, "process.exec", json.RawMessage(tc.data)), tc.code)
		})
	}
	wantCode(t, s.CheckActionResult(t.Context(), "ssh-high-interaction", 2, "start", json.RawMessage(`{"runtime_state":"running","applied_profile_revision":null}`)), "command_result_invalid")
}
