package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		case strings.Contains(call, "com.docker.compose.service=web"):
			return []byte("web-id\n"), nil
		case strings.HasPrefix(call, "inspect api-id") || strings.HasPrefix(call, "inspect redis-id") || strings.HasPrefix(call, "inspect web-id"):
			return []byte(`{"bridge":{}}`), nil
		}
		return nil, nil
	}
	d := DockerCLI{Run: run, Network: "hf-decoys", ManagementNetwork: "hf-mgmt", ProxyService: "web", WSURL: "wss://resources.localhost/assets/stream"}
	if err := d.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{"swarm init", "network create --driver overlay --attachable hf-decoys", "network create --driver overlay --attachable hf-mgmt", "network connect --alias api hf-mgmt api-id", "network connect --alias redis hf-mgmt redis-id", "network connect --alias resources.localhost hf-mgmt web-id"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in calls:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "hf-decoys api-id") || strings.Contains(joined, "hf-decoys redis-id") || strings.Contains(joined, "hf-decoys web-id") {
		t.Fatal("management services were connected to the decoy network")
	}
}

func TestPrepareRemovesLegacyManagementAttachment(t *testing.T) {
	for _, tt := range []struct {
		name            string
		disconnectError bool
	}{{"upgrade", false}, {"disconnect failure", true}} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			d := DockerCLI{Network: "decoys", ManagementNetwork: "mgmt", Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
				call := strings.Join(args, " ")
				calls = append(calls, call)
				switch {
				case strings.HasPrefix(call, "ps "):
					return []byte("api-id"), nil
				case strings.HasPrefix(call, "inspect "):
					return []byte(`{"decoys":{},"mgmt":{}}`), nil
				case strings.HasPrefix(call, "network disconnect") && tt.disconnectError:
					return nil, fmt.Errorf("disconnect denied")
				}
				return nil, nil
			}}
			err := d.connectComposeService(t.Context(), "project", "api", "api")
			if (err != nil) != tt.disconnectError {
				t.Fatalf("err=%v", err)
			}
			if !strings.Contains(strings.Join(calls, "\n"), "network disconnect decoys api-id") {
				t.Fatal("legacy management attachment remains")
			}
		})
	}
}

func TestPrepareRejectsSharedManagementNetwork(t *testing.T) {
	called := false
	d := DockerCLI{Network: "decoys", ManagementNetwork: "decoys", Run: func(context.Context, []byte, ...string) ([]byte, error) { called = true; return nil, nil }}
	if err := d.Prepare(t.Context()); err == nil || called {
		t.Fatal("unsafe networking caused Docker side effects")
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
	d := DockerCLI{Run: run, Network: "decoys", ManagementNetwork: "mgmt", WSURL: "wss://api.example/assets/stream", RedisURL: "rediss://:password@redis:6379/0", TCPImage: "registry/tcp:1", RedisImage: "registry/redis:1"}
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
	for _, want := range []string{"--replicas 1", "--read-only", "--cap-drop ALL", "--cap-add SETUID", "--network mgmt", "--hostname {{.Node.Hostname}}", "TRAP_WORKER_IMAGE=registry/tcp:1", "TRAP_WORKER_NETWORK=decoys", "/var/run/docker.sock", "honeyforge.isolation=mask3", "registry/tcp:1"} {
		if !strings.Contains(create, want) {
			t.Fatalf("service create missing %q: %s", want, create)
		}
	}
	if strings.Contains(create, "--publish") || strings.Contains(create, "--network decoys") {
		t.Fatal("management agent exposes decoy ports or joins the decoy network")
	}
}

func TestDockerDeployPassesSeparateRedisCAThroughSecret(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "redis-ca.crt")
	if err := os.WriteFile(caFile, []byte("redis-public-ca"), 0600); err != nil {
		t.Fatal(err)
	}
	var secret, service string
	d := DockerCLI{Network: "decoys", ManagementNetwork: "mgmt", WSURL: "wss://api.example/assets/stream", RedisURL: "rediss://:password@redis:6379/0", TCPImage: "registry/tcp:1", RedisCAFile: caFile,
		Run: func(_ context.Context, input []byte, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			if strings.HasPrefix(joined, "secret create ") && strings.Contains(joined, "-redis-ca") {
				secret = string(input)
			}
			if strings.HasPrefix(joined, "service create ") {
				service = joined
			}
			return nil, nil
		},
	}
	if err := d.Deploy(t.Context(), Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "tcp-banner", TypeVersion: 1}}, "private-token", 1, nil); err != nil {
		t.Fatal(err)
	}
	if secret != "redis-public-ca" || !strings.Contains(service, "AGENT_REDIS_CA_FILE=/run/secrets/redis_ca") || !strings.Contains(service, "target=redis_ca,uid=0,gid=0,mode=0400") {
		t.Fatal("Redis CA was not delivered through an isolated Docker secret")
	}
}

func TestCurrentRotatesWhenServerPinsChange(t *testing.T) {
	dir := t.TempDir()
	d := DockerCLI{CAFile: filepath.Join(dir, "ca"), ClientCAFile: filepath.Join(dir, "client-ca"), ServerPinsFile: filepath.Join(dir, "pins")}
	for _, file := range []string{d.CAFile, d.ClientCAFile, d.ServerPinsFile} {
		if err := os.WriteFile(file, []byte("initial trust"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	revision, err := d.trustRevision()
	if err != nil {
		t.Fatal(err)
	}
	id := "11111111-1111-4111-8111-111111111111"
	labels := map[string]string{"honeyforge.trap": id, "honeyforge.generation": "1", "honeyforge.certificate-expires": time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339), "honeyforge.tls-trust": revision}
	d.Run = func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		if args[0] == "service" && args[1] == "ls" {
			return []byte(serviceName(id)), nil
		}
		return json.Marshal(labels)
	}
	state, err := d.Current(t.Context(), id)
	if err != nil || !state.CertificateExpires.After(time.Now()) {
		t.Fatalf("current trust: %+v %v", state, err)
	}
	if err := os.WriteFile(d.ServerPinsFile, []byte("old and next pins"), 0600); err != nil {
		t.Fatal(err)
	}
	state, err = d.Current(t.Context(), id)
	if err != nil || !state.CertificateExpires.Before(time.Now()) {
		t.Fatalf("changed trust must trigger rotation: %+v %v", state, err)
	}
}
