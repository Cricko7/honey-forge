package agentws

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
)

type incoming struct {
	message contract.Envelope
	err     error
}

type commandLease struct {
	commandID string
	expires   time.Time
}

func (h *Handler) serveMessages(entry *sessionEntry, session *agentSession, hello AgentHello) error {
	if err := session.socket.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return fmt.Errorf("set heartbeat deadline: %w", err)
	}

	var lastPong atomic.Int64
	lastPong.Store(time.Now().UnixNano())
	session.socket.SetPongHandler(func() { lastPong.Store(time.Now().UnixNano()) })

	messages := make(chan incoming, 1)
	go func() {
		for {
			message, err := session.socket.Read(session.ctx)
			if err == nil && message.Type == "agent.heartbeat" {
				err = session.socket.SetReadDeadline(time.Now().Add(30 * time.Second))
			}
			select {
			case messages <- incoming{message, err}:
			case <-session.ctx.Done():
				return
			}
			var apiError *contract.Error
			if err != nil && (!errors.As(err, &apiError) || apiError.Code != "invalid_message") {
				return
			}
		}
	}()

	claimTicker := time.NewTicker(time.Second)
	defer claimTicker.Stop()
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	supportedActions := map[string]bool{}
	for _, kind := range hello.SupportedTypes {
		if kind.TypeID == session.identity.TypeID && kind.TypeVersion == session.identity.TypeVersion {
			for _, action := range kind.Actions {
				supportedActions[action] = true
			}
		}
	}

	var pending *commandLease
	busy := &atomic.Bool{}
	if err := h.claim(entry, session, supportedActions, &pending); err != nil {
		return err
	}

	lastPing := time.Time{}
	for {
		select {
		case <-session.ctx.Done():
			return session.ctx.Err()
		case next := <-messages:
			if next.err != nil {
				var apiError *contract.Error
				if errors.As(next.err, &apiError) && apiError.Code == "invalid_message" && contract.ValidID(string(next.message.MessageID)) {
					if err := session.replyError(next.message, next.err); err != nil {
						return err
					}
					continue
				}
				_ = session.socket.CloseCode(4408, "heartbeat_timeout")
				return nil
			}
			if err := h.handleMessage(entry, session, next.message, busy, &pending); err != nil {
				return err
			}
		case <-claimTicker.C:
			if time.Since(time.Unix(0, session.heartbeatAt.Load())) > 30*time.Second {
				_ = session.socket.CloseCode(4408, "heartbeat_timeout")
				return nil
			}
			if !lastPing.IsZero() && time.Since(lastPing) > 10*time.Second && time.Unix(0, lastPong.Load()).Before(lastPing) {
				_ = session.socket.CloseCode(4408, "heartbeat_timeout")
				return nil
			}
			if pending == nil || !time.Now().Before(pending.expires) {
				if err := h.claim(entry, session, supportedActions, &pending); err != nil {
					return err
				}
			}
		case <-pingTicker.C:
			lastPing = time.Now()
			if err := session.socket.Ping(lastPing.Add(time.Second)); err != nil {
				return fmt.Errorf("ping agent: %w", err)
			}
		}
	}
}

func (h *Handler) withActive(entry *sessionEntry, session *agentSession, action func(context.Context) error) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.current != session || !session.active.Load() {
		return context.Canceled
	}
	identity, err := h.gateway.Authenticate(session.ctx, session.token)
	if err != nil || !sameIdentity(identity, session.identity) {
		_ = session.socket.CloseCode(4401, "authentication_revoked")
		session.cancel()
		return context.Canceled
	}

	return action(session.ctx)
}

func (h *Handler) claim(entry *sessionEntry, session *agentSession, supported map[string]bool, pending **commandLease) error {
	var dispatch *commands.Dispatch
	err := h.withActive(entry, session, func(ctx context.Context) error {
		var err error
		dispatch, err = h.commands.Claim(ctx, session.identity.OrganizationID, session.identity.TrapID)
		return err
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("claim agent command: %w", err)
	}
	if dispatch == nil {
		return nil
	}
	if !supported[dispatch.Action] {
		return contract.NewError("unsupported_action")
	}

	*pending = &commandLease{commandID: dispatch.CommandID, expires: dispatch.LeaseExpiresAt}
	return session.socket.Notify(session.ctx, "command.dispatch", map[string]any{
		"command_id": dispatch.CommandID, "lease_id": dispatch.LeaseID,
		"lease_expires_at": dispatch.LeaseExpiresAt, "action": dispatch.Action,
		"params": dispatch.Params, "configuration": dispatch.Configuration,
		"expires_at": dispatch.ExpiresAt,
	})
}

func (h *Handler) handleMessage(entry *sessionEntry, session *agentSession, message contract.Envelope, busy *atomic.Bool, pending **commandLease) error {
	switch message.Type {
	case "agent.heartbeat":
		var body heartbeat
		if err := decodePayload(message.Payload, &body); err != nil {
			return session.replyError(message, err)
		}
		if err := validateRuntime(body.Runtime); err != nil {
			return session.replyError(message, err)
		}

		var state State
		err := h.withActive(entry, session, func(ctx context.Context) error {
			var err error
			state, err = h.gateway.Observe(ctx, session.identity, session.connectionID, body.Runtime)
			return err
		})
		if err != nil {
			return session.replyError(message, err)
		}
		session.heartbeatAt.Store(time.Now().UnixNano())

		return session.socket.Reply(session.ctx, message, "agent.heartbeat_ack", map[string]any{
			"server_time": time.Now().UTC(), "heartbeat_interval_seconds": interval(state),
			"desired_profile_revision": state.DesiredProfileRevision, "desired_state": desiredState(state),
		})
	case "command.progress":
		var body progress
		if err := decodePayload(message.Payload, &body); err != nil {
			return session.replyError(message, err)
		}

		var expires time.Time
		err := h.withActive(entry, session, func(ctx context.Context) error {
			var err error
			expires, err = h.commands.ExtendLease(ctx, session.identity.OrganizationID, session.identity.TrapID, body.CommandID, body.LeaseID)
			return err
		})
		if err != nil {
			return session.replyError(message, err)
		}
		if *pending != nil && (*pending).commandID == body.CommandID {
			(*pending).expires = expires
		}

		return session.socket.Reply(session.ctx, message, "command.progress_ack", map[string]any{"command_id": body.CommandID, "lease_expires_at": expires})
	case "command.result":
		if err := validateResultPayload(message.Payload); err != nil {
			return session.replyError(message, err)
		}

		var body result
		if err := decodePayload(message.Payload, &body); err != nil {
			return session.replyError(message, err)
		}
		if err := validateRuntime(body.Runtime); err != nil {
			return session.replyError(message, contract.NewError("command_result_invalid"))
		}

		input := commands.AgentResult{CommandID: body.CommandID, LeaseID: body.LeaseID, Status: body.Status, Result: body.Result, Error: body.Error, Runtime: body.Runtime}
		if string(input.Result) == "null" {
			input.Result = nil
		}
		var recordedAt time.Time
		err := h.withActive(entry, session, func(ctx context.Context) error {
			var err error
			recordedAt, err = h.commands.RecordResult(ctx, session.identity.OrganizationID, session.identity.TrapID, input)
			if err != nil {
				return err
			}

			// A replayed terminal result has its current runtime applied as a
			// heartbeat; the immutable command outcome stays unchanged.
			_, err = h.gateway.Observe(ctx, session.identity, session.connectionID, body.Runtime)
			return err
		})
		if err != nil {
			return session.replyError(message, err)
		}

		if *pending != nil && (*pending).commandID == body.CommandID {
			*pending = nil
		}
		return session.socket.Reply(session.ctx, message, "command.ack", map[string]any{"command_id": body.CommandID, "status": body.Status, "recorded_at": recordedAt})
	case "telemetry.batch":
		var body TelemetryBatch
		if err := decodePayload(message.Payload, &body); err != nil {
			return session.replyError(message, err)
		}
		if err := validateBatch(body); err != nil {
			return session.replyError(message, err)
		}
		if !busy.CompareAndSwap(false, true) {
			return session.replyError(message, contract.NewError("ingestion_busy"))
		}

		go h.ingest(entry, session, message, body, busy)
		return nil
	default:
		return session.replyError(message, contract.NewError("invalid_message"))
	}
}

func (h *Handler) ingest(entry *sessionEntry, session *agentSession, message contract.Envelope, batch TelemetryBatch, busy *atomic.Bool) {
	defer busy.Store(false)

	ctx, cancel := context.WithTimeout(session.ctx, 10*time.Second)
	defer cancel()

	entry.mu.Lock()
	active := entry.current == session && session.active.Load()
	entry.mu.Unlock()
	if !active {
		return
	}

	identity, err := h.gateway.Authenticate(ctx, session.token)
	if err != nil || !sameIdentity(identity, session.identity) {
		_ = session.socket.CloseCode(4401, "authentication_revoked")
		session.cancel()
		return
	}

	// Module 07 fences the connection_id before commit. The ingest itself runs
	// without the control lock so a slow batch cannot delay heartbeats.
	ack, err := h.gateway.Ingest(ctx, session.identity, session.connectionID, batch)
	if !session.active.Load() {
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_ = session.replyError(message, contract.NewError("ingestion_pending"))
		return
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = contract.NewError("ingestion_pending")
		}
		_ = session.replyError(message, err)
		return
	}
	if ack.BatchID != batch.BatchID || len(ack.AcknowledgedEventIDs) != len(batch.Events) || ack.StoredAt.IsZero() {
		_ = session.replyError(message, contract.NewError("ingestion_pending"))
		return
	}
	for i, event := range batch.Events {
		if ack.AcknowledgedEventIDs[i] != event.EventID {
			_ = session.replyError(message, contract.NewError("ingestion_pending"))
			return
		}
	}

	busy.Store(false)
	_ = session.socket.Reply(session.ctx, message, "telemetry.ack", ackPayload(ack))
}

func ackPayload(ack TelemetryAck) map[string]any {
	return map[string]any{"batch_id": ack.BatchID, "acknowledged_event_ids": ack.AcknowledgedEventIDs, "stored_at": ack.StoredAt}
}
