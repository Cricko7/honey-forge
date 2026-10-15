package orchestrator

import (
	"context"
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestDeployConpot(t *testing.T) {
	var create []string
	d := DockerCLI{Network: "decoys", ManagementNetwork: "mgmt", WSURL: "wss://api/assets/stream", RedisURL: "rediss://redis", ConpotImage: "hf/conpot:pinned", Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "service" && args[1] == "create" {
			create = append([]string(nil), args...)
		}
		return nil, nil
	}}
	target := Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "conpot", TypeVersion: 1}}
	if err := d.Deploy(t.Context(), target, "agent-secret", 1, []int{161}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(create, " ")
	if !strings.Contains(joined, "NM_WORKER_IMAGE=hf/conpot:pinned") || strings.Contains(joined, "--publish") {
		t.Fatal(joined)
	}
	d.ConpotImage = ""
	if err := d.Deploy(t.Context(), target, "agent-secret", 1, []int{161}); err == nil {
		t.Fatal("accepted missing Conpot image")
	}
}
