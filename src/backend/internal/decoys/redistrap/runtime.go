package redistrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type Sink func(context.Context, events.AgentEvent) error

type Runtime struct {
	cancel      context.CancelFunc
	listener    net.Listener
	wg          sync.WaitGroup
	mu          sync.Mutex
	connections map[net.Conn]bool
	failure     error
	done        chan struct{}
}

func Start(ctx context.Context, snapshot profiles.Snapshot, host string, sink Sink) (*Runtime, error) {
	config, err := ParseConfig(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if sink == nil {
		return nil, fmt.Errorf("telemetry sink is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	service := config.Services[0]
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(service.Port)))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("bind Redis service: %w", err)
	}
	r := &Runtime{cancel: cancel, listener: listener, connections: map[net.Conn]bool{}, done: make(chan struct{})}
	stop := context.AfterFunc(ctx, r.closeConnections)
	sessions := make(chan struct{}, 64)
	r.wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if ctx.Err() == nil {
					r.fail(err)
				}
				return
			}
			select {
			case sessions <- struct{}{}:
			case <-ctx.Done():
				conn.Close()
				return
			}
			r.mu.Lock()
			if ctx.Err() != nil {
				r.mu.Unlock()
				<-sessions
				conn.Close()
				return
			}
			r.connections[conn] = true
			r.mu.Unlock()
			r.wg.Go(func() {
				defer func() { <-sessions }()
				defer func() { conn.Close(); r.mu.Lock(); delete(r.connections, conn); r.mu.Unlock() }()
				if err := serve(ctx, conn, service.Name, service.Port, service.Password, snapshot, sink); err != nil {
					r.fail(err)
				}
			})
		}
	})
	go func() { r.wg.Wait(); stop(); close(r.done) }()
	return r, nil
}

func (r *Runtime) closeConnections() {
	r.listener.Close()
	r.mu.Lock()
	defer r.mu.Unlock()
	for conn := range r.connections {
		conn.Close()
	}
}

func (r *Runtime) fail(err error) {
	r.mu.Lock()
	if r.failure == nil {
		r.failure = err
	}
	r.mu.Unlock()
	r.cancel()
}

func (r *Runtime) Err() error { r.mu.Lock(); defer r.mu.Unlock(); return r.failure }
func (r *Runtime) Stop()      { r.cancel(); <-r.done }

type emitter struct {
	ctx      context.Context
	sink     Sink
	snapshot profiles.Snapshot
	source   *net.TCPAddr
	port     int
	session  string
	sequence int64
}

func (e *emitter) emit(kind string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode service event: %w", err)
	}
	e.sequence++
	// The final close event must survive cancellation of the runtime.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(e.ctx), 5*time.Second)
	defer cancel()
	return e.sink(ctx, events.AgentEvent{
		EventID: string(contract.NewID()), EventType: kind, TypeID: "redis-emulator", TypeVersion: 1,
		ProfileRevision: int64(e.snapshot.ProfileRevision), OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: e.session, SessionSequence: e.sequence,
		Source:      events.Source{IP: e.source.IP.String(), Port: e.source.Port},
		Destination: events.Destination{Protocol: "tcp", Port: e.port}, Data: raw,
	})
}

func newEmitter(ctx context.Context, conn net.Conn, port int, snapshot profiles.Snapshot, sink Sink) (*emitter, error) {
	source, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return nil, errors.New("unsupported service address")
	}
	return &emitter{ctx: ctx, sink: sink, snapshot: snapshot, source: source, port: port, session: string(contract.NewID())}, nil
}
