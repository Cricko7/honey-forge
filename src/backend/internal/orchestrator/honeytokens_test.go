package orchestrator

import (
	"context"
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestDeployHoneytokens(t *testing.T) {
	var create []string
	d := DockerCLI{Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "service" && args[1] == "create" {
			create = append([]string(nil), args...)
		}
		return nil, nil
	}, Network: "decoys", ManagementNetwork: "mgmt", WSURL: "wss://api.example/assets/stream", RedisURL: "rediss://:password@redis:6379/0", HoneytokenImage: "registry/honeytokens:1"}
	if err := d.Deploy(t.Context(), Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "honeytoken-http", TypeVersion: 1}}, "agent-secret", 1, []int{8080}); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(create, " ")
	if !strings.Contains(args, "TRAP_WORKER_IMAGE=registry/honeytokens:1") || strings.Contains(args, "--publish") {
		t.Fatal(args)
	}
}
