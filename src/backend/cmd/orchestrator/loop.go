package main

import (
	"context"
	"log/slog"
	"time"
)

func runLoop(ctx context.Context, prepare, reconcile, cleanup func(context.Context) error) error {
	var preparedAt, cleanedAt time.Time
	delay := 3 * time.Second
	for ctx.Err() == nil {
		cycle, cancel := context.WithTimeout(ctx, 60*time.Second)
		var err error
		if preparedAt.IsZero() || time.Since(preparedAt) >= 5*time.Minute {
			err = prepare(cycle)
			if err == nil {
				preparedAt = time.Now()
				cleanedAt = preparedAt
			}
		} else if time.Since(cleanedAt) >= 30*time.Second {
			err = cleanup(cycle)
			if err == nil {
				cleanedAt = time.Now()
			}
		}

		if err == nil {
			err = reconcile(cycle)
		}

		cancel()
		wait := 3 * time.Second
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "deployment reconciliation failed", "error", err)
			preparedAt = time.Time{}
			wait = delay
			delay = min(delay*2, time.Minute)
		} else {
			delay = 3 * time.Second
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}

	return nil
}
