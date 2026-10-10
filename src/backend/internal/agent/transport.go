package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/commands"
)

var ErrRevoked = errors.New("agent credentials are revoked")

type wire struct {
	conn *websocket.Conn
}

func (w *wire) send(kind string, payload any) (contract.ID, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode agent payload: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	id := contract.NewID()
	if err := w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}
	return id, w.conn.WriteJSON(contract.Envelope{MessageID: id, Type: kind, Payload: fields})
}

func payload(message contract.Envelope, dst any) error {
	raw, err := json.Marshal(message.Payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func session(ctx context.Context, opts Options, j *Journal, s *Service, local *Local, p *Process) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, TLSClientConfig: opts.TLS, Subprotocols: []string{"resource-stream.v1"}}
	conn, response, err := dialer.DialContext(ctx, opts.URL, http.Header{"Authorization": []string{"Bearer " + opts.Token}})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return ErrRevoked
		}
		return fmt.Errorf("connect backend: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetReadLimit(contract.MaxBodyBytes)
	w := wire{conn: conn}
	supported := opts.SupportedTypes
	if len(supported) == 0 {
		supported = []agentws.SupportedType{{TypeID: "tcp-banner", TypeVersion: 1, Actions: []string{"start", "stop", "apply_config"}}}
	}
	helloID, err := w.send("agent.hello", agentws.AgentHello{BootID: opts.BootID, AgentVersion: "1.0", Hostname: opts.Hostname, SupportedTypes: supported, Runtime: s.Runtime()})
	if err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	welcome, err := contract.DecodeEnvelope(raw)
	if err != nil {
		return err
	}
	var body struct {
		TrapID   string `json:"trap_id"`
		Interval int    `json:"heartbeat_interval_seconds"`
	}
	if welcome.Type != "agent.welcome" || welcome.ReplyTo == nil || *welcome.ReplyTo != helloID || payload(welcome, &body) != nil || body.TrapID != opts.TrapID {
		return fmt.Errorf("invalid backend welcome")
	}
	if body.Interval < 5 || body.Interval > 10 {
		return fmt.Errorf("invalid heartbeat interval")
	}
	incoming := make(chan contract.Envelope, 1)
	readErrors := make(chan error, 1)
	go func() {
		for {
			if err := conn.SetReadDeadline(time.Now().Add(35 * time.Second)); err != nil {
				readErrors <- err
				return
			}
			kind, raw, err := conn.ReadMessage()
			if err != nil {
				readErrors <- err
				return
			}
			if kind != websocket.TextMessage {
				readErrors <- fmt.Errorf("unsupported backend frame")
				return
			}
			message, err := contract.DecodeEnvelope(raw)
			if err != nil {
				readErrors <- err
				return
			}
			select {
			case incoming <- message:
			case <-ctx.Done():
				return
			}
		}
	}()
	heartbeat := time.NewTicker(time.Duration(body.Interval) * time.Second)
	defer heartbeat.Stop()
	flush := time.NewTicker(s.FlushInterval())
	defer flush.Stop()
	var pending *Batch
	var pendingMessage contract.ID
	var sentAt time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErrors:
			var closed *websocket.CloseError
			if errors.As(err, &closed) && (closed.Code == 4401 || closed.Code == 4409) {
				return ErrRevoked
			}
			return err
		case code := <-local.Failures:
			s.Failed(code)
		case <-heartbeat.C:
			if err := p.Poll(); err != nil {
				s.Failed("runtime_start_failed")
			}
			if _, err := w.send("agent.heartbeat", map[string]any{"runtime": s.Runtime()}); err != nil {
				return err
			}
		case <-flush.C:
			s.RecoverBuffer()
			if pending != nil && time.Since(sentAt) > 10*time.Second {
				return fmt.Errorf("telemetry acknowledgement timed out")
			}
			if pending == nil {
				batches := j.Pending()
				if len(batches) > 0 {
					batch := batches[0]
					id, err := w.send("telemetry.batch", batch)
					if err != nil {
						return err
					}
					pending = &batch
					pendingMessage = id
					sentAt = time.Now()
				}
			}
		case message := <-incoming:
			switch message.Type {
			case "telemetry.ack":
				if pending == nil || message.ReplyTo == nil || *message.ReplyTo != pendingMessage {
					return fmt.Errorf("unexpected telemetry acknowledgement")
				}
				var ack agentws.TelemetryAck
				if err := payload(message, &ack); err != nil {
					return err
				}
				if ack.BatchID != pending.BatchID || ack.StoredAt.IsZero() {
					return fmt.Errorf("invalid telemetry acknowledgement")
				}
				if err := j.Ack(ack.BatchID, ack.AcknowledgedEventIDs); err != nil {
					s.Failed("buffer_unavailable")
					return err
				}
				pending = nil
			case "command.dispatch":
				var dispatch commands.Dispatch
				if err := payload(message, &dispatch); err != nil {
					return err
				}
				if !contract.ValidID(dispatch.CommandID) || !contract.ValidID(dispatch.LeaseID) {
					return fmt.Errorf("invalid command identity")
				}
				if _, err := w.send("command.progress", map[string]string{"command_id": dispatch.CommandID, "lease_id": dispatch.LeaseID}); err != nil {
					return err
				}
				result, err := s.Execute(ctx, dispatch)
				if err != nil {
					return fmt.Errorf("execute command: %w", err)
				}
				flush.Reset(s.FlushInterval())
				if _, err := w.send("command.result", result); err != nil {
					return err
				}
			case "error":
				var rejection struct {
					Error commands.RuntimeError `json:"error"`
				}
				if payload(message, &rejection) == nil && (rejection.Error.Code == "telemetry_invalid" || rejection.Error.Code == "event_id_conflict" || rejection.Error.Code == "batch_conflict") {
					s.Failed("telemetry_invalid")
				}
				return fmt.Errorf("backend rejected agent message")
			case "agent.heartbeat_ack", "command.progress_ack", "command.ack":
			default:
				return fmt.Errorf("unexpected backend message")
			}
		}
	}
}
