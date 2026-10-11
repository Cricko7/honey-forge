package beacon

import (
	"testing"
	"time"
)

func TestDelay(t *testing.T) {
	for _, tc := range []struct {
		name string
		base time.Duration
	}{
		{"flush minimum", 100 * time.Millisecond},
		{"heartbeat minimum", 5 * time.Second},
		{"heartbeat maximum", 10 * time.Second},
		{"server ping", 15 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 100 {
				got := Delay(tc.base)
				if got < tc.base-tc.base/5 || got > tc.base+tc.base/5 {
					t.Fatalf("delay %s outside ±20%% of %s", got, tc.base)
				}
			}
		})
	}
}
