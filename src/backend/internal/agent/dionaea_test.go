package agent

import (
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestDionaeaWorkerIsolation(t *testing.T) {
	p := Process{WorkerImage: "hf/dionaea:pinned", WorkerNetwork: "decoys", TrapID: "11111111-1111-4111-8111-111111111111"}
	s := profiles.Snapshot{TypeID: "dionaea", TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{
		"services":   []any{map[string]any{"name": "files", "port": 1445, "protocol": "smb"}},
		"session":    map[string]any{"max_connections": 16, "max_seconds": 300},
		"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 500},
	}}
	args, err := p.workerArgs(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--publish 0.0.0.0:1445:1445/tcp", "--memory 256m --memory-swap 256m", "--read-only", "--security-opt no-new-privileges", "/run/dionaea:rw,nosuid,nodev,noexec,size=8m,mode=0700,uid=65532,gid=65532", "NM_STDIO=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, bad := range []string{"--secret", "--mount", "docker.sock", "--privileged", "--network host", "NM_TOKEN", "NM_WS_URL", "/udp"} {
		if strings.Contains(joined, bad) {
			t.Errorf("unwanted privilege or transport %s", bad)
		}
	}
}
