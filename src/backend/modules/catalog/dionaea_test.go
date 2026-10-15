package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

const dionaeaConfig = `{"services":[{"name":"files","port":1445,"protocol":"smb"}],"session":{"max_connections":16,"max_seconds":300},"management":{"heartbeat_interval_seconds":5,"telemetry_flush_interval_ms":500}}`

func TestDionaeaConfigurationBoundary(t *testing.T) {
	s := testService(t)
	for _, protocol := range []string{"smb", "mssql", "mysql", "mqtt", "http", "memcache"} {
		t.Run(protocol, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "dionaea", 1, json.RawMessage(strings.Replace(dionaeaConfig, `"smb"`, `"`+protocol+`"`, 1))), "")
		})
	}
	for _, tc := range []struct{ name, raw string }{
		{"unknown protocol", strings.Replace(dionaeaConfig, `"smb"`, `"ftp"`, 1)},
		{"invalid port", strings.Replace(dionaeaConfig, "1445", "0", 1)},
		{"unbounded sessions", strings.Replace(dionaeaConfig, `"max_connections":16`, `"max_connections":1000`, 1)},
		{"external exporter", strings.Replace(dionaeaConfig, `"services":`, `"hpfeeds":"https://evil.invalid","services":`, 1)},
		{"native config override", strings.Replace(dionaeaConfig, `"services":`, `"config_path":"/tmp/untrusted","services":`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "dionaea", 1, json.RawMessage(tc.raw)), "config_invalid")
		})
	}
	wantCode(t, s.CheckActionResult(t.Context(), "dionaea", 1, "start", json.RawMessage(`{"runtime_state":"running","applied_profile_revision":null}`)), "command_result_invalid")
}
