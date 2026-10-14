package orchestrator

import (
	"context"
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestDeployPinsCompanyNode(t *testing.T) {
	for _, kind := range []string{"tcp-banner", "redis-emulator", "honeytoken-http", "custom-http", "ssh-high-interaction"} {
		t.Run(kind, func(t *testing.T) {
			var command string
			d := DockerCLI{Network: "decoys", ManagementNetwork: "mgmt", WorkerBindIP: "192.0.2.5", TCPImage: "tcp", RedisImage: "redis", HoneytokenImage: "http", HTTPImage: "python-http", SSHImage: "ssh", SandboxImage: "sandbox", WSURL: "wss://center/assets/stream", RedisURL: "rediss://redis", Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
				if len(args) > 1 && args[0] == "service" && args[1] == "create" {
					command = strings.Join(args, " ")
				}

				return nil, nil
			}}

			target := Target{ID: "trap", OrganizationID: "org", DeploymentNodeID: "company-node", Snapshot: profiles.Snapshot{TypeID: kind, TypeVersion: 1}}
			if err := d.Deploy(t.Context(), target, "token", 1, []int{2222}); err != nil {
				t.Fatal(err)
			}

			for _, want := range []string{"--constraint node.id==company-node", "--constraint node.labels.honeyforge.organization==org", "com.nodemetrics.node=company-node"} {
				if !strings.Contains(command, want) {
					t.Fatalf("missing %q in %s", want, command)
				}
			}

			if strings.Contains(command, "node.role==manager") {
				t.Fatal("company worker excluded")
			}

			if strings.Contains(command, "NM_BIND_IP=192.0.2.5") || kind != "ssh-high-interaction" && !strings.Contains(command, "NM_BIND_IP=0.0.0.0") {
				t.Fatal("platform bind address leaked into company placement")
			}
		})
	}
}
