package orchestrator

import (
	"context"
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestPrepareBootstrapsSingleNodeAndConnectsComposeServices(t *testing.T) {
	var calls []string
	run := func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		switch {
		case strings.HasPrefix(call, "info "):
			return []byte("inactive\n"), nil
		case strings.HasPrefix(call, "network ls "):
			return nil, nil
		case strings.HasPrefix(call, "container inspect "):
			return []byte("demo\n"), nil
		case strings.Contains(call, "com.docker.compose.service=api"):
			return []byte("api-id\n"), nil
		case strings.Contains(call, "com.docker.compose.service=redis"):
			return []byte("redis-id\n"), nil
		case strings.HasPrefix(call, "inspect api-id") || strings.HasPrefix(call, "inspect redis-id"):
			return []byte(`{"bridge":{}}`), nil
		}
		return nil, nil
	}
	d := DockerCLI{Run: run, Network: "hf-decoys"}
	if err := d.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{"swarm init", "network create --driver overlay --attachable hf-decoys", "network connect --alias api hf-decoys api-id", "network connect --alias redis hf-decoys redis-id"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in calls:\n%s", want, joined)
		}
	}
}

func TestDockerDeployUsesOneIsolatedSwarmTaskAndSecret(t *testing.T) {
	var calls [][]string
	var secretInput string
	run := func(_ context.Context, input []byte, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) >= 2 && args[0] == "secret" && args[1] == "create" && strings.Contains(strings.Join(args, " "), "-token") {
			secretInput = string(input)
		}
		return nil, nil
	}
	d := DockerCLI{Run: run, Network: "decoys", WSURL: "wss://api.example/assets/stream", RedisURL: "redis://redis:6379/0", TCPImage: "registry/tcp:1", RedisImage: "registry/redis:1"}
	if err := d.Deploy(t.Context(), Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "tcp-banner", TypeVersion: 1}}, "private-token", 1, []int{2222}); err != nil {
		t.Fatal(err)
	}
	if secretInput != "private-token" {
		t.Fatal("token was not passed through secret stdin")
	}
	var create string
	for _, args := range calls {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "private-token") {
			t.Fatal("token leaked into command arguments")
		}
		if len(args) >= 2 && args[0] == "service" && args[1] == "create" {
			create = joined
		}
	}
	for _, want := range []string{"--replicas 1", "--read-only", "--cap-drop ALL", "--cap-add SETUID", "--network decoys", "--hostname {{.Node.Hostname}}", "--publish published=2222,target=2222,protocol=tcp,mode=host", "registry/tcp:1"} {
		if !strings.Contains(create, want) {
			t.Fatalf("service create missing %q: %s", want, create)
		}
	}
}
