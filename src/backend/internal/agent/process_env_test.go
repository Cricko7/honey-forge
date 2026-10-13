package agent

import (
	"reflect"
	"testing"
)

func TestChildEnvironmentContainsOnlyRuntimeSettings(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin", "SystemRoot=C:\\Windows", "TMPDIR=/tmp", "LANG=C.UTF-8",
		"NM_TOKEN=private", "NM_TOKEN_FILE=/run/secrets/nm_token", "NM_WS_URL=wss://private",
		"NM_STORE_URL=rediss://private", "POSTGRES_PASSWORD=private", "AWS_SECRET_ACCESS_KEY=private",
		"AGENT_TOKEN=private", "TRAP_TOKEN=private", "HTTP_PROXY=http://private", "GODEBUG=private",
		"DOCKER_HOST=unix:///var/run/docker.sock", "DOCKER_CONFIG=/private",
		"NM_ROLE=ssh-high-interaction", "NM_SANDBOX_IMAGE=hf/sandbox:local",
	}

	for _, tc := range []struct {
		name    string
		gateway bool
		want    []string
	}{
		{"decoy", false, parent[:4]},
		{"trusted SSH gateway", true, append(append([]string{}, parent[:4]...), parent[len(parent)-4:]...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := childEnvironment(parent, tc.gateway); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("child inherited unexpected settings: %v", got)
			}
		})
	}
}
