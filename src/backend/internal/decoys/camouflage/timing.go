package camouflage

import (
	"context"
	"math/rand/v2"
	"time"
)

// Response adds a small scheduling/network delay without a fixed timing signature.
func Response(ctx context.Context) bool {
	return wait(ctx, time.Millisecond, 8*time.Millisecond)
}

// Authentication models password verification, including rejected credentials.
func Authentication(ctx context.Context) bool {
	return wait(ctx, 100*time.Millisecond, 300*time.Millisecond)
}

func wait(ctx context.Context, min, max time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}

	timer := time.NewTimer(min + time.Duration(rand.Int64N(int64(max-min)+1)))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
