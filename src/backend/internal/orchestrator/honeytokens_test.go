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
	}, Network: "decoys", WSURL: "wss://api.example/assets/stream", RedisURL: "redis://redis:6379/0", HoneytokenImage: "registry/honeytokens:1"}
	if err := d.Deploy(t.Context(), Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "honeytoken-http", TypeVersion: 1}}, "agent-secret", 1, []int{8080}); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(create, " ")
	if !strings.Contains(args, "registry/honeytokens:1") || !strings.Contains(args, "published=8080,target=8080") {
		t.Fatal(args)
	}
}
