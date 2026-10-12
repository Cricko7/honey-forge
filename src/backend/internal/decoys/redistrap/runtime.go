package redistrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/decoys/internal/tcpserver"
	"honey-forge/internal/platform/traffic"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type Sink func(context.Context, events.AgentEvent) error

type Runtime struct{ server *tcpserver.Server }

func (r *Runtime) Stop() {
	r.server.Stop()
}

func (r *Runtime) Err() error {
	return r.server.Err()
}

func Start(ctx context.Context, snapshot profiles.Snapshot, host string, sink Sink) (*Runtime, error) {
	config, err := ParseConfig(ctx, snapshot)
	if err != nil {
		return nil, err
	}

	if sink == nil {
		return nil, fmt.Errorf("telemetry sink is required")
	}

	service := config.Services[0]
	listener, err := traffic.Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(service.Port)))
	if err != nil {
		return nil, fmt.Errorf("bind Redis service: %w", err)
	}

	identity := newServerIdentity(service.Port)
	listeners := []tcpserver.Listener{{Listener: listener, Serve: func(ctx context.Context, conn net.Conn) error {
		return serve(ctx, conn, service.Name, service.Port, service.Password, snapshot, sink, identity)
	}}}

	return &Runtime{tcpserver.Start(ctx, listeners, 64)}, nil
}

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
		EventID:         string(contract.NewID()),
		EventType:       kind,
		TypeID:          "redis-emulator",
		TypeVersion:     1,
		ProfileRevision: int64(e.snapshot.ProfileRevision),
		OccurredAt:      time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:       e.session,
		SessionSequence: e.sequence,
		Source:          events.Source{IP: e.source.IP.String(), Port: e.source.Port},
		Destination:     events.Destination{Protocol: "tcp", Port: e.port},
		Data:            raw,
	})
}

func newEmitter(ctx context.Context, conn net.Conn, port int, snapshot profiles.Snapshot, sink Sink) (*emitter, error) {
	source, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return nil, errors.New("unsupported service address")
	}

	return &emitter{ctx: ctx, sink: sink, snapshot: snapshot, source: source, port: port, session: string(contract.NewID())}, nil
}
