package sshtrap

import (
	"strings"
	"testing"
)

func TestSandboxArgumentsNeverExposeHostOrControlPlane(t *testing.T) {
	args, err := sandboxArgs("11111111-1111-4111-8111-111111111111", "hf/sandbox:local", "ssh-ed25519 public-key", 300)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--network none", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges", "--memory 128m", "--cpus 0.5", "--pids-limit 64", "--user 1000:1000", "--tmpfs /home/sandbox:", "--label honeyforge.sandbox=true"} {
		if !strings.Contains(joined, flag) {
			t.Fatalf("missing isolation %s: %s", flag, joined)
		}
	}
	for _, bad := range []string{"--privileged", "--volume", "--mount", "--publish", "AGENT_", "docker.sock", "--network host"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("unsafe argument: %s", bad)
		}
	}
	if _, err := sandboxArgs("../../host", "image", "key", 300); err == nil {
		t.Fatal("invalid identity accepted")
	}
}
