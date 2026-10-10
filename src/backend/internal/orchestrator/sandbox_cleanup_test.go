package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSandboxCleanupOnlyRemovesExpiredOwnedContainers(t *testing.T) {
	var removed []string
	d := DockerCLI{Run: func(_ context.Context, _ []byte, args ...string) ([]byte, error) {
		switch args[0] {
		case "ps":
			return []byte("expired\nactive\n"), nil
		case "container":
			if args[2] == "expired" {
				return []byte("/hf-session-11111111-1111-4111-8111-111111111111 100"), nil
			}
			return []byte("/hf-session-22222222-2222-4222-8222-222222222222 300"), nil
		case "rm":
			removed = append(removed, args[2])
		}
		return nil, nil
	}}
	if err := d.CleanupSandboxes(t.Context(), time.Unix(200, 0)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(removed, ",") != "expired" {
		t.Fatalf("removed %v", removed)
	}
}
