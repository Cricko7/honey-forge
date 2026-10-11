package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLimiterBoundsConcurrentAuthenticationAcrossIPs(t *testing.T) {
	limiter := newAuthLimiter()
	router := gin.New()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	t.Cleanup(func() { close(release); wg.Wait() })

	router.POST("/sessions", limiter.middleware, func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusCreated)
	})

	for _, ip := range []string{"192.0.2.1:1234", "192.0.2.2:1234"} {
		request := httptest.NewRequest(http.MethodPost, "/sessions", nil)
		request.RemoteAddr = ip
		wg.Go(func() { router.ServeHTTP(httptest.NewRecorder(), request) })
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("authentication request did not reach the handler")
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/sessions", nil)
	request.RemoteAddr = "192.0.2.3:1234"
	response := httptest.NewRecorder()
	done := make(chan struct{})
	wg.Go(func() { router.ServeHTTP(response, request); close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a third concurrent request must be rejected before reaching expensive authentication")
	}
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" || !strings.Contains(response.Body.String(), "rate_limited") {
		t.Fatalf("overload response: %d %s", response.Code, response.Body.String())
	}
}

func TestLimiterReleasesAuthenticationCapacity(t *testing.T) {
	limiter := newAuthLimiter()
	router := gin.New()
	router.POST("/sessions", limiter.middleware, func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	for range 3 {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/sessions", nil))
		if response.Code != http.StatusCreated {
			t.Fatalf("capacity was not released: %d %s", response.Code, response.Body.String())
		}
	}
}

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
