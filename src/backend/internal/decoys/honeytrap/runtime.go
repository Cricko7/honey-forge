package honeytrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type Sink func(context.Context, events.AgentEvent) error

type Runtime struct {
	ctx      context.Context
	snapshot profiles.Snapshot
	config   Config
	sink     Sink
	cancel   context.CancelFunc
	server   *http.Server
	done     chan struct{}
	mu       sync.Mutex
	failure  error
}

func Start(ctx context.Context, snapshot profiles.Snapshot, host string, sink Sink) (*Runtime, error) {
	config, err := ParseConfig(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if sink == nil {
		return nil, fmt.Errorf("event sink is required")
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(config.Services[0].Port)))
	if err != nil {
		return nil, fmt.Errorf("listen HTTP service: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &Runtime{ctx: ctx, snapshot: snapshot, config: config, sink: sink, cancel: cancel, done: make(chan struct{})}
	r.server = &http.Server{
		Handler: http.HandlerFunc(r.serveHTTP), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	served := make(chan struct{})
	go func() {
		<-ctx.Done()
		shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		if err := r.server.Shutdown(shutdown); err != nil {
			r.fail(err)
			if err := r.server.Close(); err != nil {
				r.fail(err)
			}
		}
		<-served
		close(r.done)
	}()
	go func() {
		defer close(served)
		if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.fail(err)
		}
		cancel()
	}()

	return r, nil
}

func (r *Runtime) fail(err error) {
	r.mu.Lock()
	if r.failure == nil {
		r.failure = err
	}
	r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
	}
}

func (r *Runtime) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failure
}

func (r *Runtime) Stop() {
	r.cancel()
	<-r.done
}
