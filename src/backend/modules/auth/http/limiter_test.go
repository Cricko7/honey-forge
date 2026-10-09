package http

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterConcurrentAttemptsAndExpiry(t *testing.T) {
	t.Parallel()

	limiter := newAuthLimiter()
	now := time.Now()
	var allowed atomic.Int32
	var wg sync.WaitGroup

	for range 100 {
		wg.Go(func() {
			if ok, _ := limiter.allow("127.0.0.1", now); ok {
				allowed.Add(1)
			}
		})
	}

	wg.Wait()

	if allowed.Load() != authLimit {
		t.Fatalf("allowed %d requests, want %d", allowed.Load(), authLimit)
	}

	if ok, retry := limiter.allow("127.0.0.1", now); ok || retry < 1 {
		t.Fatal("limit must reject further attempts with a retry delay")
	}

	if ok, _ := limiter.allow("127.0.0.1", now.Add(time.Minute)); !ok {
		t.Fatal("attempts must be allowed after the window expires")
	}
}

func TestLimiterUsesRollingMinute(t *testing.T) {
	t.Parallel()

	limiter := newAuthLimiter()
	now := time.Now()
	limiter.allow("127.0.0.1", now)

	for range authLimit - 1 {
		limiter.allow("127.0.0.1", now.Add(59*time.Second))
	}

	if ok, _ := limiter.allow("127.0.0.1", now.Add(time.Minute)); !ok {
		t.Fatal("the oldest attempt should expire")
	}
	if ok, _ := limiter.allow("127.0.0.1", now.Add(time.Minute)); ok {
		t.Fatal("a fixed-window boundary must not allow another burst")
	}
}
