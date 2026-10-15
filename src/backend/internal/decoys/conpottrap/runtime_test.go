package conpottrap

import (
	"context"
	"honey-forge/modules/events"
	"strings"
	"testing"
)

func TestStartRequiresIsolatedWorker(t *testing.T) {
	t.Setenv("NM_STDIO", "")
	_, err := Start(t.Context(), testSnapshot(t), "", func(context.Context, events.AgentEvent) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "isolated") {
		t.Fatalf("unisolated start: %v", err)
	}
}
