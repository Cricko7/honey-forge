package camouflage

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestDelays(t *testing.T) {
	for _, tc := range []struct {
		name     string
		wait     func(context.Context) bool
		min, max time.Duration
	}{
		{"response", Response, time.Millisecond, 8 * time.Millisecond},
		{"authentication", Authentication, 100 * time.Millisecond, 300 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				if !tc.wait(t.Context()) {
					t.Fatal("unexpected cancellation")
				}

				if elapsed := time.Since(start); elapsed < tc.min || elapsed > tc.max {
					t.Fatalf("delay %v outside [%v, %v]", elapsed, tc.min, tc.max)
				}

				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				start = time.Now()
				if tc.wait(ctx) || time.Since(start) != 0 {
					t.Fatal("cancelled delay did not exit immediately")
				}

				ctx, cancel = context.WithCancel(t.Context())
				finished := make(chan bool, 1)
				go func() {
					finished <- tc.wait(ctx)
				}()
				synctest.Wait()
				cancel()
				if <-finished {
					t.Fatal("pending delay ignored cancellation")
				}
			})
		})
	}
}
