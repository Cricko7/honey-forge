package agent

import (
	"strings"
	"testing"
)

func TestWorkerContainerHasNoManagementAccess(t *testing.T) {
	p := Process{WorkerImage: "hf/tcp:local", WorkerNetwork: "nm-decoys", TrapID: "11111111-1111-4111-8111-111111111111"}
	args, err := p.workerArgs(t.Context(), demoSnapshot())
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(args, " ")
	for _, want := range []string{"--network nm-decoys", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges", "--publish 0.0.0.0:2222:2222/tcp", "NM_STDIO=1", "hf/tcp:local --worker"} {
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
