// Package stream owns the shared WSS text-envelope transport, not feature payloads.
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
)

const (
	FrontendPath = "/api/stream"
	AgentPath    = "/assets/stream"
)

type Socket struct {
	conn       *websocket.Conn
	writeMu    sync.Mutex
	capability contract.Capability
}

func Upgrade(c *gin.Context, capability contract.Capability, policies ...*contract.BrowserPolicy) (*Socket, error) {
	if _, ok := contract.PrincipalFrom(c.Request.Context()); !ok {
		code := "unauthenticated"
		if capability == contract.AgentStream {
			code = "agent_unauthenticated"
		}
		return nil, fail(c, code)
	}
	if capability != contract.FrontendStream && capability != contract.AgentStream {
		return nil, fail(c, "forbidden")
	}
	if c.Request.TLS == nil {
		return nil, fail(c, "origin_not_allowed")
	}
	if capability == contract.AgentStream {
		if len(c.Request.Header.Values("Origin")) > 0 {
			return nil, fail(c, "origin_not_allowed")
		}
		protocols := c.Request.Header.Values("Sec-WebSocket-Protocol")
		if len(protocols) != 1 || protocols[0] != "resource-stream.v1" {
			return nil, fail(c, "invalid_ws_protocol")
		}
	}
	if capability == contract.FrontendStream {
		origins := c.Request.Header.Values("Origin")
		if len(origins) != 1 {
			return nil, fail(c, "origin_not_allowed")
		}
		protocols := c.Request.Header.Values("Sec-WebSocket-Protocol")
		if len(protocols) != 1 || protocols[0] != "dashboard-stream.v1" {
			return nil, fail(c, "invalid_ws_protocol")
		}
	}
	if len(policies) > 0 && policies[0] != nil {
		if !policies[0].CheckOrigin(c) {
			return nil, contract.NewError("origin_not_allowed")
		}
	} else {
		if values := c.Request.Header.Values("Origin"); len(values) > 0 && (len(values) != 1 || values[0] != "https://"+c.Request.Host) {
			return nil, fail(c, "origin_not_allowed")
		}
	}
	if err := contract.AuthorizeCapability(c.Request.Context(), capability); err != nil {
		contract.Fail(c, err.(*contract.Error))
		return nil, err
	}
	if c.Request.URL.RawQuery != "" {
		return nil, fail(c, "invalid_query")
	}
	upgrader := websocket.Upgrader{EnableCompression: false, HandshakeTimeout: 5 * time.Second, CheckOrigin: func(*http.Request) bool { return true }, Error: func(http.ResponseWriter, *http.Request, int, error) {
		contract.Fail(c, contract.NewError("invalid_json"))
	}}
	if capability == contract.AgentStream {
		upgrader.Subprotocols = []string{"resource-stream.v1"}
	}
	if capability == contract.FrontendStream {
		upgrader.Subprotocols = []string{"dashboard-stream.v1"}
	}
	headers := http.Header{}
	headers.Set("X-Request-ID", c.Writer.Header().Get("X-Request-ID"))
	headers.Set("Cache-Control", "no-store")
	conn, err := upgrader.Upgrade(c.Writer, c.Request, headers)
	if err != nil {
		return nil, fmt.Errorf("upgrade stream: %w", err)
	}
	conn.EnableWriteCompression(false)
	return &Socket{conn: conn, capability: capability}, nil
}

func fail(c *gin.Context, code string) error {
	err := contract.NewError(code)
	contract.Fail(c, err)
	return err
}

func (s *Socket) Close() error { return s.conn.Close() }

func (s *Socket) SetReadDeadline(deadline time.Time) error {
	return s.conn.SetReadDeadline(deadline)
}

func (s *Socket) SetPongHandler(handler func()) {
	s.conn.SetPongHandler(func(string) error {
		handler()
		return nil
	})
}

func (s *Socket) Ping(deadline time.Time) error {
	return s.conn.WriteControl(websocket.PingMessage, nil, deadline)
}

func (s *Socket) CloseCode(code int, reason string) error {
	return s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
}

func (s *Socket) Read(ctx context.Context) (contract.Envelope, error) {
	if err := ctx.Err(); err != nil {
		return contract.Envelope{}, err
	}
	stop := context.AfterFunc(ctx, func() {
		if err := s.conn.Close(); err != nil { /* Close is best effort after cancellation. */
		}
	})
	defer stop()
	kind, reader, err := s.conn.NextReader()
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return contract.Envelope{}, canceled
		}
		return contract.Envelope{}, fmt.Errorf("read stream: %w", err)
	}
	if kind != websocket.TextMessage {
		if err := s.closeCode(contract.WSCloseUnsupportedData); err != nil {
			return contract.Envelope{}, err
		}
		return contract.Envelope{}, fmt.Errorf("binary stream message")
	}
	b, err := io.ReadAll(io.LimitReader(reader, contract.MaxBodyBytes+1))
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return contract.Envelope{}, canceled
		}
		return contract.Envelope{}, fmt.Errorf("read stream payload: %w", err)
	}
	if len(b) > contract.MaxBodyBytes {
		if err := s.CloseCode(contract.WSCloseTooLarge, "message_too_large"); err != nil {
			return contract.Envelope{}, err
		}
		return contract.Envelope{}, fmt.Errorf("oversized stream message")
	}
	if !utf8.Valid(b) {
		if s.capability == contract.FrontendStream {
			return contract.Envelope{}, contract.NewError("invalid_message")
		}
		if err := s.closeCode(websocket.CloseInvalidFramePayloadData); err != nil {
			return contract.Envelope{}, err
		}
		return contract.Envelope{}, contract.NewError("invalid_json")
	}
	envelope, err := contract.DecodeEnvelope(b)
	if err != nil {
		if s.capability == contract.AgentStream || s.capability == contract.FrontendStream {
			var request struct {
				MessageID contract.ID `json:"message_id"`
			}
			if json.Unmarshal(b, &request) == nil && contract.ValidID(string(request.MessageID)) {
				return contract.Envelope{MessageID: request.MessageID}, contract.NewError("invalid_message")
			}
			if s.capability == contract.FrontendStream {
				return contract.Envelope{}, contract.NewError("invalid_message")
			}
		}
		if closeErr := s.closeCode(websocket.ClosePolicyViolation); closeErr != nil {
			return contract.Envelope{}, closeErr
		}
		return contract.Envelope{}, err
	}
	if envelope.ReplyTo != nil {
		if s.capability == contract.AgentStream || s.capability == contract.FrontendStream {
			return envelope, contract.NewError("invalid_message")
		}
		if err := s.closeCode(websocket.ClosePolicyViolation); err != nil {
			return contract.Envelope{}, err
		}
		return contract.Envelope{}, contract.NewError("validation_failed")
	}
	return envelope, nil
}

func (s *Socket) Write(ctx context.Context, envelope contract.Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}
	if _, err := contract.DecodeEnvelope(b); err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() {
		if err := s.conn.Close(); err != nil { /* Connection teardown after cancellation is best effort. */
		}
	})
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("stream deadline: %w", err)
	}
	if err := s.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		return fmt.Errorf("write stream: %w", err)
	}
	return nil
}

func (s *Socket) Reply(ctx context.Context, request contract.Envelope, messageType string, payload map[string]any) error {
	return s.send(ctx, &request.MessageID, messageType, payload)
}

func (s *Socket) Notify(ctx context.Context, messageType string, payload map[string]any) error {
	return s.send(ctx, nil, messageType, payload)
}

func (s *Socket) send(ctx context.Context, reply *contract.ID, messageType string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("payload object: %w", err)
	}
	return s.Write(ctx, contract.Envelope{MessageID: contract.NewID(), Type: messageType, ReplyTo: reply, Payload: raw})
}

func (s *Socket) closeCode(code int) error {
	reason := "invalid_message"
	if code == contract.WSCloseUnsupportedData {
		reason = "unsupported_data"
	}
	if err := s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second)); err != nil {
		return fmt.Errorf("close stream: %w", err)
	}
	return nil
}
