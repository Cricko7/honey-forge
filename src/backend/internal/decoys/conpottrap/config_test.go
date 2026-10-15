package conpottrap

import (
	"encoding/json"
	"testing"

	"honey-forge/modules/profiles"
)

func testSnapshot(t *testing.T) profiles.Snapshot {
	t.Helper()
	s := profiles.Snapshot{TypeID: "conpot", TypeVersion: 1, ProfileRevision: 2}
	if err := json.Unmarshal([]byte(`{"services":[{"name":"plc","port":1502,"protocol":"modbus"}],"session":{"max_connections":16,"max_seconds":300},"management":{"heartbeat_interval_seconds":5,"telemetry_flush_interval_ms":500}}`), &s.Config); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseConfig(t *testing.T) {
	s := testSnapshot(t)
	c, err := ParseConfig(t.Context(), s)
	if err != nil || c.Services[0].Port != 1502 || c.Services[0].Protocol != "modbus" {
		t.Fatalf("configuration = %+v, %v", c, err)
	}
	s.TypeVersion = 2
	if _, err := ParseConfig(t.Context(), s); err == nil {
		t.Fatal("accepted unknown version")
	}
}
