package main

import (
	"context"
	"log/slog"
	"time"
)

// Each SSH supervisor sweeps its own host, including sessions left by a crashed supervisor.
func sandboxCleanup(ctx context.Context, clean func(context.Context, time.Time) error) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for ctx.Err() == nil {
		cycle, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := clean(cycle, time.Now()); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "remove expired SSH sandboxes", "error", err)
		}

		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
