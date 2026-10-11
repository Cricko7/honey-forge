package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"honey-forge/internal/platform/traffic"
	"honey-forge/modules/events"
)

type LocalClient struct {
	traffic        chan traffic.Frame
	TrafficDropped atomic.Uint64
	conn           *websocket.Conn
	mu             sync.Mutex // serializes event/ack exchanges
	replies        chan map[string]json.RawMessage
	done           chan struct{}
	readErr        error // read only after done closes
}

func DialLocal(ctx context.Context, address, token string, certificate []byte) (*LocalClient, error) {
	return dialLocalPipe(ctx, address, token, certificate, nil)
}

// A decoy's TLS client uses stdin/stdout instead of a management network socket.
// Certificate pinning and the short-lived local credential still apply.
func dialLocalPipe(ctx context.Context, address, token string, certificate []byte, pipe net.Conn) (*LocalClient, error) {
	config, err := localClientTLS(address, certificate)
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, TLSClientConfig: config}
	if pipe != nil {
		dialer.NetDialContext = func(context.Context, string, string) (net.Conn, error) { return pipe, nil }
	}
	conn, response, err := dialer.DialContext(ctx, address, http.Header{"Authorization": []string{"Bearer " + token}})
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			return nil, fmt.Errorf("close local handshake: %w", errors.Join(closeErr, conn.Close()))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("connect trap to agent: %w", err)
	}
	conn.SetReadLimit(16 * 1024)
	c := &LocalClient{conn: conn, replies: make(chan map[string]json.RawMessage, 1), done: make(chan struct{})}
	c.traffic = make(chan traffic.Frame, traffic.QueueCapacity)
	go c.readReplies()
	go c.sendTraffic()
	return c, nil
}

func (c *LocalClient) readReplies() {
	defer close(c.done)
	for {
		var reply map[string]json.RawMessage
		if err := c.conn.ReadJSON(&reply); err != nil {
			c.readErr = err
			return
		}
		select {
		case c.replies <- reply:
		default:
			c.readErr = errors.Join(fmt.Errorf("unexpected local acknowledgement"), c.conn.Close())
			return
		}
	}
}

func (c *LocalClient) Done() <-chan struct{} { return c.done }

func (c *LocalClient) exchange(ctx context.Context, payload any, field, want string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() {
		if err := c.conn.Close(); err != nil {
			slog.DebugContext(ctx, "local telemetry connection closed", "error", err)
		}
	})
	defer stop()
	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if err := c.conn.WriteJSON(payload); err != nil {
		return fmt.Errorf("send trap message: %w", err)
	}
	if err := c.conn.SetWriteDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear local write deadline: %w", err)
	}
	var reply map[string]json.RawMessage
	select {
	case reply = <-c.replies:
	case <-c.done:
		// Preserve the final acknowledgement even when EOF arrives immediately.
		select {
		case reply = <-c.replies:
		default:
			return fmt.Errorf("read local acknowledgement: %w", c.readErr)
		}
	case <-ctx.Done():
		return fmt.Errorf("local acknowledgement timeout: %w", errors.Join(ctx.Err(), c.conn.Close()))
	}
	if field == "ready" {
		if string(reply[field]) != "true" {
			return fmt.Errorf("trap readiness rejected")
		}
		return nil
	}
	var got string
	if json.Unmarshal(reply[field], &got) != nil || got != want {
		return fmt.Errorf("event was not locally committed")
	}
	return nil
}

func (c *LocalClient) Emit(ctx context.Context, e events.AgentEvent) error {
	return c.exchange(ctx, e, "event_id", e.EventID)
}
func (c *LocalClient) Ready(ctx context.Context) error {
	return c.exchange(ctx, map[string]string{"type": "trap.ready"}, "ready", "")
}
func (c *LocalClient) Close() error { return c.conn.Close() }
