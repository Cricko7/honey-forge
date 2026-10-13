package redistrap

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"honey-forge/modules/events"
)

func TestHELLOAuthenticationTelemetry(t *testing.T) {
	var got []events.AgentEvent
	e := &emitter{ctx: t.Context(), source: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}, port: 6380, snapshot: testSnapshot(6380), sink: func(_ context.Context, event events.AgentEvent) error {
		got = append(got, event)
		return nil
	}}
	if err := emitCommand(e, "redis", []string{"HELLO", "3", "AUTH", "default", "wrong", "SETNAME", "client"}, 100, false, nil, "rejected"); err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].EventType != "service.auth_attempt" || got[1].EventType != "service.action" || got[1].SessionSequence != 2 {
		t.Fatalf("missing RESP3 authentication or command telemetry: %+v", got)
	}

	var auth struct{ Username, Password, Outcome string }
	if err := json.Unmarshal(got[0].Data, &auth); err != nil || auth.Username != "default" || auth.Password != "wrong" || auth.Outcome != "rejected" {
		t.Fatalf("auth=%+v err=%v", auth, err)
	}

	want := errors.New("journal unavailable")
	e.sink = func(context.Context, events.AgentEvent) error { return want }
	if err := emitCommand(e, "redis", []string{"AUTH", "wrong"}, 12, false, nil, "rejected"); !errors.Is(err, want) {
		t.Fatalf("lost telemetry failure: %v", err)
	}
}

type closedClient struct{ net.Conn }

func (closedClient) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (closedClient) SetReadDeadline(time.Time) error {
	return net.ErrClosed
}

func (closedClient) Close() error {
	return nil
}

func TestClosedClientDoesNotFailRedisRuntime(t *testing.T) {
	backend, peer := net.Pipe()
	defer peer.Close()

	err := serve(t.Context(), closedClient{}, "redis", 6380, testSnapshot(6380), func(context.Context, events.AgentEvent) error {
		return nil
	}, func(context.Context) (net.Conn, error) {
		return backend, nil
	})
	if err != nil {
		t.Fatalf("closed client failed the entire runtime: %v", err)
	}
}
