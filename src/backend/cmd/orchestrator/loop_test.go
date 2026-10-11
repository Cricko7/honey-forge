package main

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestLoopDoesNotPrepareDockerEveryCycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		prepares, cycles, cleanups := 0, 0, 0
		err := runLoop(ctx, func(context.Context) error { prepares++; return nil }, func(context.Context) error {
			cycles++
			if cycles == 10 {
				cancel()
			}
			return nil
		}, func(context.Context) error { cleanups++; return nil })
		if err != nil || prepares != 1 || cycles != 10 || cleanups != 0 {
			t.Fatalf("prepare=%d cycles=%d cleanup=%d err=%v", prepares, cycles, cleanups, err)
		}
	})
}

func TestLoopWaitsAfterSlowCycleAndBacksOffFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		start := time.Now()
		calls := 0
		err := runLoop(ctx, func(context.Context) error {
			calls++
			if calls == 4 {
				cancel()
			}
			return errors.New("Docker unavailable")
		}, func(context.Context) error { t.Fatal("reconciled without Docker preparation"); return nil }, func(context.Context) error { return nil })
		if err != nil || calls != 4 || time.Since(start) != 21*time.Second {
			t.Fatalf("calls=%d elapsed=%v err=%v", calls, time.Since(start), err)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var ended time.Time
		calls := 0
		err := runLoop(ctx, func(context.Context) error { return nil }, func(context.Context) error {
			calls++
			if calls == 1 {
				time.Sleep(10 * time.Second)
				ended = time.Now()
			} else {
				if time.Since(ended) < 3*time.Second {
					t.Fatal("no rest after slow cycle")
				}
				cancel()
			}
			return nil
		}, func(context.Context) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
	})
}
