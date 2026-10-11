// Package beacon varies periodic management traffic within bounded deadlines.
package beacon

import (
	"math/rand/v2"
	"time"
)

// Delay samples a fresh interval within ±20% of the configured base.
func Delay(base time.Duration) time.Duration {
	spread := base / 5
	return base - spread + time.Duration(rand.Int64N(int64(2*spread)+1))
}
