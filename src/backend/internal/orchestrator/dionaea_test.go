package orchestrator

import (
	"context"
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestDeployDionaea(t *testing.T) {
	var create []string
	d := DockerCLI{Network: "decoys", ManagementNetwork: "mgmt", WSURL: "wss://api/assets/stream", RedisURL: "rediss://redis", DionaeaImage: "hf/dionaea:pinned", Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "service" && args[1] == "create" {
			create = append([]string(nil), args...)
		}
		return nil, nil
	}}
	target := Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "dionaea", TypeVersion: 1}}
	if err := d.Deploy(t.Context(), target, "agent-secret", 1, []int{1445}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(create, " ")
	if !strings.Contains(joined, "NM_WORKER_IMAGE=hf/dionaea:pinned") || strings.Contains(joined, "--publish") {
		t.Fatal(joined)
	}
	d.DionaeaImage = ""
	if err := d.Deploy(t.Context(), target, "agent-secret", 1, []int{1445}); err == nil {
		t.Fatal("accepted missing Dionaea image")
	}
}
