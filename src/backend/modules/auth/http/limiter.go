package http

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/src/backend/internal/platform/httpx"
)

const (
	authLimit     = 10
	maxTrackedIPs = 4096
)

type authLimiter struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	nextCleanup time.Time
}

func newAuthLimiter() *authLimiter {
	return &authLimiter{attempts: make(map[string][]time.Time)}
}

func (l *authLimiter) allow(ip string, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-time.Minute)
	if !now.Before(l.nextCleanup) {
		for key, times := range l.attempts {
			if !times[len(times)-1].After(cutoff) {
				delete(l.attempts, key)
			}
		}

		l.nextCleanup = now.Add(time.Minute)
	}

	times, exists := l.attempts[ip]
	if !exists && len(l.attempts) >= maxTrackedIPs {
		return false, 60
	}

	for len(times) > 0 && !times[0].After(cutoff) {
		times = times[1:]
	}
	if len(times) >= authLimit {
		retry := math.Ceil(times[0].Add(time.Minute).Sub(now).Seconds())

		return false, max(1, int(retry))
	}

	l.attempts[ip] = append(times, now)

	return true, 0
}

func (l *authLimiter) middleware(c *gin.Context) {
	if allowed, retry := l.allow(c.ClientIP(), time.Now()); !allowed {
		c.Header("Retry-After", strconv.Itoa(retry))
		httpx.WriteError(c, http.StatusTooManyRequests, "rate_limited", "Too many requests")
		return
	}

	c.Next()
}
