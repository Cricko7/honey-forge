package agent

import (
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestWorkerContainerHasNoManagementAccess(t *testing.T) {
	p := Process{WorkerImage: "hf/tcp:local", WorkerNetwork: "nm-decoys", TrapID: "11111111-1111-4111-8111-111111111111"}
	args, err := p.workerArgs(t.Context(), demoSnapshot())
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(args, " ")
	for _, want := range []string{"--log-driver local", "--log-opt max-size=10m", "--log-opt max-file=3", "--network nm-decoys", "--hostname service", "--dns 127.0.0.1", "--dns-search .", "--ipc private", "--cgroupns private", "--ulimit core=0:0", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges", "--publish 0.0.0.0:2222:2222/tcp", "NM_STDIO=1", "hf/tcp:local --worker"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q: %s", want, joined)
		}
	}

	for _, bad := range []string{"nm-mgmt", "AGENT_", "docker.sock", "--secret", "--mount", "--privileged", "--network host"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("worker crosses management boundary: %s", bad)
		}
	}
}

func TestRedisWorkerPrivateSocketDirectory(t *testing.T) {
	p := Process{WorkerImage: "hf/redis:local", WorkerNetwork: "nm-decoys", TrapID: "11111111-1111-4111-8111-111111111111"}
	snapshot := profiles.Snapshot{TypeID: "redis-emulator", TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{
		"services":   []any{map[string]any{"name": "redis", "port": 6380, "password": "bait-pass"}},
		"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
	}}
	args, err := p.workerArgs(t.Context(), snapshot)
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--tmpfs /run/redis:rw,nosuid,nodev,noexec,size=1m,mode=0700,uid=65532,gid=65532") {
		t.Fatalf("missing private Redis socket directory: %s", joined)
	}

	for _, forbidden := range []string{"docker.sock", "--mount", "--secret", "--network host", "bait-pass"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("Redis worker leaks management or bait credentials: %s", joined)
		}
	}
}

func TestWorkerContainerRejectsIncompleteIsolation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		process Process
	}{
		{"missing network", Process{WorkerImage: "image"}},
		{"invalid identity", Process{WorkerImage: "image", WorkerNetwork: "decoys", TrapID: "../../host"}},
		{"host network", Process{WorkerImage: "image", WorkerNetwork: "host", TrapID: "11111111-1111-4111-8111-111111111111"}},
		{"invalid bind address", Process{
			WorkerImage:   "image",
			WorkerNetwork: "decoys",
			TrapID:        "11111111-1111-4111-8111-111111111111",
			WorkerBindIP:  "host:1234",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.process.workerArgs(t.Context(), demoSnapshot()); err == nil {
				t.Fatal("accepted unsafe worker configuration")
			}
		})
	}
}
