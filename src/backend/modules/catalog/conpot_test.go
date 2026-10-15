package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

const conpotConfig = `{"services":[{"name":"plc","port":1502,"protocol":"modbus"}],"session":{"max_connections":16,"max_seconds":300},"management":{"heartbeat_interval_seconds":5,"telemetry_flush_interval_ms":500}}`

func TestConpotConfigurationBoundary(t *testing.T) {
	s := testService(t)
	for _, tc := range []struct{ name, config, code string }{
		{"modbus", conpotConfig, ""},
		{"S7", strings.Replace(conpotConfig, "modbus", "s7comm", 1), ""},
		{"SNMP", strings.Replace(conpotConfig, "modbus", "snmp", 1), ""},
		{"BACnet", strings.Replace(conpotConfig, "modbus", "bacnet", 1), ""},
		{"EtherNet/IP", strings.Replace(conpotConfig, "modbus", "enip", 1), ""},
		{"unknown protocol", strings.Replace(conpotConfig, "modbus", "smtp", 1), "config_invalid"},
		{"invalid port", strings.Replace(conpotConfig, "1502", "0", 1), "config_invalid"},
		{"unbounded connections", strings.Replace(conpotConfig, "\"max_connections\":16", "\"max_connections\":1000", 1), "config_invalid"},
		{"raw template", strings.Replace(conpotConfig, `"services":`, `"template":"/tmp/evil","services":`, 1), "config_invalid"},
		{"external sink", strings.Replace(conpotConfig, `"services":`, `"hpfeeds":"attacker.invalid","services":`, 1), "config_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, s.CheckConfig(t.Context(), "conpot", 1, json.RawMessage(tc.config)), tc.code)
		})
	}
	// Conpot obeys the same pinned-revision lifecycle as the other runtimes.
	wantCode(t, s.CheckActionResult(t.Context(), "conpot", 1, "start", json.RawMessage(`{"runtime_state":"running","applied_profile_revision":null}`)), "command_result_invalid")
}
