package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

var ErrRevoked = errors.New("agent credentials are revoked")

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
		TrapID        string             `json:"trap_id"`
		Interval      int                `json:"heartbeat_interval_seconds"`
		Configuration *profiles.Snapshot `json:"current_configuration"`
	}
	if welcome.Type != "agent.welcome" || welcome.ReplyTo == nil || *welcome.ReplyTo != helloID || payload(welcome, &body) != nil || body.TrapID != opts.TrapID {
		return fmt.Errorf("invalid backend welcome")
	}
	if body.Interval < 5 || body.Interval > 10 {
		return fmt.Errorf("invalid heartbeat interval")
	}
	if err := s.RestoreWelcome(ctx, body.Configuration); err != nil {
		return fmt.Errorf("restore welcome: %w", err)
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
	heartbeat := time.NewTicker(s.HeartbeatInterval())
	defer heartbeat.Stop()
	flush := time.NewTicker(s.FlushInterval())
	defer flush.Stop()
	var pending *Batch
	var pendingMessage contract.ID
	var sentAt time.Time
	resultMessages := map[contract.ID]string{}
	acknowledgedResults := map[string]commands.Status{}
	resultSent := map[string]time.Time{}
	sendResult := func(result commands.AgentResult) error {
		id, err := w.send("command.result", result)
		if err != nil {
			return err
		}
		resultMessages[id] = result.CommandID
		resultSent[result.CommandID] = time.Now()
		return nil
	}
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
			if err := s.PollRunner(p); err != nil {
				s.Failed("runtime_start_failed")
			}
			if _, err := w.send("agent.heartbeat", map[string]any{"runtime": s.Runtime()}); err != nil {
				return err
			}
		case <-flush.C:
			if err := j.CheckStorage(ctx); err != nil {
				s.Failed("buffer_unavailable")
				continue
			}
			s.RecoverBuffer()
			for _, result := range s.PendingResults() {
				if time.Since(resultSent[result.CommandID]) >= 2*time.Second {
					if err := sendResult(result); err != nil {
						return err
					}
				}
			}
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
				heartbeat.Reset(s.HeartbeatInterval())
				if err := sendResult(result); err != nil {
					return err
				}
			case "error":
				var rejection struct {
					Error commands.RuntimeError `json:"error"`
				}
				if payload(message, &rejection) == nil && (rejection.Error.Code == "telemetry_invalid" || rejection.Error.Code == "event_id_conflict" || rejection.Error.Code == "batch_conflict") {
					s.Failed("telemetry_invalid")
				}
				if rejection.Error.Code == "stale_command_lease" && message.ReplyTo != nil {
					if id, ok := resultMessages[*message.ReplyTo]; ok {
						resultSent[id] = time.Now().Add(24 * time.Hour)
						continue
					}
				}
				return fmt.Errorf("backend rejected agent message")
			case "command.ack":
				var ack struct {
					CommandID  string          `json:"command_id"`
					Status     commands.Status `json:"status"`
					RecordedAt time.Time       `json:"recorded_at"`
				}
				if message.ReplyTo == nil || payload(message, &ack) != nil || ack.RecordedAt.IsZero() || (resultMessages[*message.ReplyTo] != ack.CommandID && acknowledgedResults[ack.CommandID] != ack.Status) {
					return fmt.Errorf("invalid command acknowledgement")
				}
				if err := s.AckResult(ack.CommandID, ack.Status); err != nil {
					s.Failed("buffer_unavailable")
					return err
				}
				for id, commandID := range resultMessages {
					if commandID == ack.CommandID {
						delete(resultMessages, id)
					}
				}
				delete(resultSent, ack.CommandID)
				acknowledgedResults[ack.CommandID] = ack.Status
			case "agent.heartbeat_ack", "command.progress_ack":
			default:
				return fmt.Errorf("unexpected backend message")
			}
		}
	}
}
