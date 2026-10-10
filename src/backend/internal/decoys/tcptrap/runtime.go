package tcptrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type Sink func(context.Context, events.AgentEvent) error

type Runtime struct {
	cancel      context.CancelFunc
	listeners   []net.Listener
	wg          sync.WaitGroup
	mu          sync.Mutex
	connections map[net.Conn]bool
	failure     error
	done        chan struct{}
}

// Start binds every listener before reporting success. No partial launch is kept.
func Start(ctx context.Context, snapshot profiles.Snapshot, host string, sink Sink) (*Runtime, error) {
	config, err := ParseConfig(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if sink == nil {
		return nil, fmt.Errorf("telemetry sink is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &Runtime{cancel: cancel, connections: map[net.Conn]bool{}, done: make(chan struct{})}
	for _, l := range config.Listeners {
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(host, fmtPort(l.Port)))
		if err != nil {
			cancel()
			for _, bound := range r.listeners {
				bound.Close()
			}
			return nil, fmt.Errorf("bind TCP listener: %w", err)
		}
		r.listeners = append(r.listeners, listener)
	}
	stop := context.AfterFunc(ctx, func() { r.closeConnections() })
	sessions := make(chan struct{}, 64)
	for i, l := range config.Listeners {
		bound := r.listeners[i]
		r.wg.Go(func() {
			for {
				conn, err := bound.Accept()
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
					<-sessions
					r.mu.Unlock()
					conn.Close()
					return
				}
				r.connections[conn] = true
				r.mu.Unlock()
				// Bound concurrent sessions; each handler exits on idle timeout or cancellation.
				r.wg.Go(func() {
					defer func() { <-sessions }()
					defer func() { conn.Close(); r.mu.Lock(); delete(r.connections, conn); r.mu.Unlock() }()
					if err := serve(ctx, conn, l, config, snapshot, sink); err != nil {
						r.fail(err)
					}
				})
			}
		})
	}
	go func() { r.wg.Wait(); stop(); close(r.done) }()
	return r, nil
}

func (r *Runtime) closeConnections() {
	for _, l := range r.listeners {
		l.Close()
	}
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

func (r *Runtime) Stop() { r.cancel(); <-r.done }

func serve(ctx context.Context, conn net.Conn, l Listener, config Config, snapshot profiles.Snapshot, sink Sink) error {
	source, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("unsupported TCP address")
	}
	session := string(contract.NewID())
	sequence := int64(0)
	started := time.Now()
	emit := func(kind string, data map[string]any) error {
		raw, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("encode TCP event: %w", err)
		}
		sequence++
		// Finish metadata can still be committed after the runtime context is canceled.
		eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return sink(eventCtx, events.AgentEvent{EventID: string(contract.NewID()), EventType: kind, TypeID: "tcp-banner", TypeVersion: 1, ProfileRevision: int64(snapshot.ProfileRevision), OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), SessionID: session, SessionSequence: sequence, Source: events.Source{IP: source.IP.String(), Port: source.Port}, Destination: events.Destination{Protocol: "tcp", Port: l.Port}, Data: raw})
	}
	if err := emit("tcp.connection_opened", map[string]any{"listener_name": l.Name}); err != nil {
		return err
	}
	reason := "banner_sent"
	var received int64
	interaction := func() error {
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		if _, err := io.WriteString(conn, l.Banner); err != nil {
			reason = "network_error"
			return nil
		}
		if l.CloseAfterBanner {
			return nil
		}
		captured := 0
		buffer := make([]byte, 4096)
		for {
			if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
				return err
			}
			n, err := conn.Read(buffer)
			if n > 0 {
				received += int64(n)
				if config.Logging.Capture && captured < config.Logging.MaxBytes {
					count := min(n, config.Logging.MaxBytes-captured)
					if err := emit("tcp.payload_received", map[string]any{"listener_name": l.Name, "payload_base64": base64.StdEncoding.EncodeToString(buffer[:count]), "captured_bytes": count, "original_bytes": n, "truncated": count < n}); err != nil {
						return err
					}
					captured += count
				}
			}
			if err != nil {
				switch {
				case ctx.Err() != nil:
					reason = "service_stopped"
				case errors.Is(err, io.EOF):
					reason = "peer_closed"
				default:
					var netErr net.Error
					if errors.As(err, &netErr) && netErr.Timeout() {
						reason = "idle_timeout"
					} else {
						reason = "network_error"
					}
				}
				return nil
			}
		}
	}
	err := interaction()
	if err != nil {
		reason = "service_stopped"
	}
	closeErr := emit("tcp.connection_closed", map[string]any{"listener_name": l.Name, "duration_ms": time.Since(started).Milliseconds(), "bytes_received": received, "reason": reason})
	return errors.Join(err, closeErr)
}
