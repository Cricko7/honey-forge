package enginepipe

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
