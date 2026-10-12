package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestSandboxCleanupRetriesAndStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		var calls atomic.Int32
		go func() {
			defer close(done)

			sandboxCleanup(ctx, func(context.Context, time.Time) error {
				calls.Add(1)

				return errors.New("temporary Docker outage")
			})
		}()

		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("startup cleanup calls=%d", calls.Load())
		}

		time.Sleep(30 * time.Second)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatalf("retry cleanup calls=%d", calls.Load())
		}

		cancel()
		<-done
		time.Sleep(30 * time.Second)
		if calls.Load() != 2 {
			t.Fatalf("cleanup continued after shutdown: %d", calls.Load())
		}
	})
}
