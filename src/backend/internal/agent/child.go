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

type childBootstrap struct {
	URL      string            `json:"url"`
	Token    string            `json:"token"`
	Snapshot profiles.Snapshot `json:"snapshot"`
}

func readChildBootstrap(reader io.Reader) (childBootstrap, error) {
	var bootstrap childBootstrap
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bootstrap); err != nil {
		return bootstrap, fmt.Errorf("read trap bootstrap: %w", err)
	}
	if bootstrap.URL == "" || bootstrap.Token == "" {
		return bootstrap, fmt.Errorf("incomplete trap bootstrap")
	}
	return bootstrap, nil
}

func RunTrap(ctx context.Context) error {
	bootstrap, err := readChildBootstrap(os.Stdin)
	if err != nil {
		return err
	}
	client, err := DialLocal(ctx, bootstrap.URL, bootstrap.Token)
	if err != nil {
		return err
	}
	defer client.Close()
	runtime, err := tcptrap.Start(ctx, bootstrap.Snapshot, "", client.Emit)
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
