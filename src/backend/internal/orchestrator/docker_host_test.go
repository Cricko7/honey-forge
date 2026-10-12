package orchestrator

import (
	"context"
	"strings"
	"testing"

	"honey-forge/modules/profiles"
)

func TestDeployLocalWSHost(t *testing.T) {
	for _, tc := range []struct {
		name, ip string
		invalid  bool
	}{
		{"DNS default", "", false},
		{"gateway", "172.19.0.1", false},
		{"loopback", "127.0.0.1", true},
		{"unspecified", "0.0.0.0", true},
		{"multicast", "224.0.0.1", true},
		{"invalid", "host-gateway", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			var service string
			d := DockerCLI{Network: "decoys", ManagementNetwork: "mgmt", TCPImage: "registry/tcp:1", WSURL: "wss://resources.example:443/assets/stream", WSHostIP: tc.ip, RedisURL: "rediss://redis:6379"}
			d.Run = func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
				calls++
				if len(args) > 1 && args[0] == "service" && args[1] == "create" {
					service = strings.Join(args, " ")
				}
				return nil, nil
			}
			err := d.Deploy(t.Context(), Target{ID: "11111111-1111-4111-8111-111111111111", Snapshot: profiles.Snapshot{TypeID: "tcp-banner", TypeVersion: 1}}, "private-token", 1, nil)
			if tc.invalid {
				if err == nil || calls != 0 {
					t.Fatalf("invalid mapping must fail before Docker mutations: err=%v calls=%d", err, calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(service, "NM_WS_URL=wss://resources.example:443/assets/stream") {
				t.Fatal("TLS endpoint changed")
			}
			if tc.ip == "" {
				if strings.Contains(service, "--host ") {
					t.Fatal("default DNS overridden")
				}
			} else if !strings.Contains(service, "--host resources.example:172.19.0.1") {
				t.Fatal("host mapping not passed to Swarm")
			}
		})
	}
}
