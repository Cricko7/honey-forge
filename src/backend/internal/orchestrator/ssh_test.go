package orchestrator

import (
	"context"
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestSSHDeploySeparatesTrustedGatewayAndSandbox(t *testing.T) {
	var create string
	d := DockerCLI{
		Network:           "decoys",
		ManagementNetwork: "mgmt",
		WSURL:             "wss://api/assets/stream",
		RedisURL:          "rediss://:password@redis:6379",
		SSHImage:          "hf/ssh:local",
		SandboxImage:      "hf/sandbox:local",
		Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
			if len(args) > 1 && args[0] == "service" && args[1] == "create" {
				create = strings.Join(args, " ")
			}

			return nil, nil
		},
	}
	if err := d.Deploy(t.Context(), Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "ssh-high-interaction", TypeVersion: 1}}, "token", 1, []int{2222}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"hf/ssh:local", "NM_SANDBOX_IMAGE=hf/sandbox:local", "/var/run/docker.sock", "--constraint node.role==manager"} {
		if !strings.Contains(create, want) {
			t.Fatalf("missing %s in %s", want, create)
		}
	}
}

func TestSSHHostTelemetryDeployment(t *testing.T) {
	for _, tc := range []struct {
		name, sensor string
		invalid      bool
	}{
		{name: "configured", sensor: "tetragon"},
		{name: "missing", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var create string
			d := DockerCLI{Network: "decoys", ManagementNetwork: "mgmt", WSURL: "wss://api/assets/stream", RedisURL: "rediss://:password@redis:6379", SSHImage: "hf/ssh:local", SandboxImage: "hf/sandbox:local", TetragonContainer: tc.sensor,
				Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
					if len(args) > 1 && args[0] == "service" && args[1] == "create" {
						create = strings.Join(args, " ")
					}
					return nil, nil
				},
			}
			err := d.Deploy(t.Context(), Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "ssh-high-interaction", TypeVersion: 2}}, "token", 7, []int{2222})
			if (err != nil) != tc.invalid {
				t.Fatalf("error=%v", err)
			}
			if tc.invalid {
				if create != "" {
					t.Fatal("unconfigured sensor deployed")
				}
				return
			}
			for _, want := range []string{"NM_TETRAGON_CONTAINER=tetragon", "NM_DEPLOYMENT_GENERATION=7", "hf/ssh:local"} {
				if !strings.Contains(create, want) {
					t.Fatalf("missing %s", want)
				}
			}
		})
	}
}
