package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestConpotWorkerIsolation(t *testing.T) {
	s := profiles.Snapshot{TypeID: "conpot", TypeVersion: 1, ProfileRevision: 1}
	if err := json.Unmarshal([]byte(`{"services":[{"name":"plc","port":1502,"protocol":"modbus"}],"session":{"max_connections":16,"max_seconds":300},"management":{"heartbeat_interval_seconds":5,"telemetry_flush_interval_ms":500}}`), &s.Config); err != nil {
		t.Fatal(err)
	}
	p := Process{WorkerImage: "hf/conpot:local", WorkerNetwork: "decoys", TrapID: "11111111-1111-4111-8111-111111111111"}
	args, err := p.workerArgs(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--read-only", "--cap-drop ALL", "--security-opt no-new-privileges", "--publish 0.0.0.0:1502:1502/tcp", "--memory 256m --memory-swap 256m", "NM_STDIO=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, bad := range []string{"--secret", "--mount", "docker.sock", "--privileged", "--network host", "NM_TOKEN", "NM_WS_URL", "--publish 0.0.0.0:102:"} {
		if strings.Contains(joined, bad) {
			t.Errorf("unexpected management access or listener: %s", bad)
		}
	}
	s.Config["services"] = []any{map[string]any{"name": "plc", "port": 161, "protocol": "snmp"}}
	args, err = p.workerArgs(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--publish 0.0.0.0:161:161/udp") || strings.Contains(joined, "161:161/tcp") {
		t.Fatal("incorrect UDP publication", joined)
	}
}
