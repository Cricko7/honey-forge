package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"honey-forge/internal/tcptrap"
	"honey-forge/modules/profiles"
)

func RunTrap(ctx context.Context) error {
	var snapshot profiles.Snapshot
	if err := json.Unmarshal([]byte(os.Getenv("TRAP_SNAPSHOT")), &snapshot); err != nil {
		return fmt.Errorf("load trap configuration: %w", err)
	}
	client, err := DialLocal(ctx, os.Getenv("TRAP_AGENT_URL"), os.Getenv("TRAP_AGENT_TOKEN"))
	if err != nil {
		return err
	}
	defer client.Close()
	runtime, err := tcptrap.Start(ctx, snapshot, "", client.Emit)
	if err != nil {
		return err
	}
	defer runtime.Stop()
	if err := client.Ready(ctx); err != nil {
		return err
	}
	// Parent pipe EOF also stops an orphaned trap when the agent exits or crashes.
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, os.Stdin); close(done) }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		case <-ticker.C:
			if err := runtime.Err(); err != nil {
				return fmt.Errorf("trap runtime failed: %w", err)
			}
		}
	}
}
