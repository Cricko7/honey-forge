package agentws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/stream"
	"honey-forge/modules/commands"
)

type Handler struct {
	gateway  Gateway
	commands CommandService
	registry sessionRegistry
}

func NewHandler(gateway Gateway, commands CommandService) *Handler {
	return &Handler{gateway: gateway, commands: commands, registry: sessionRegistry{entries: map[string]*sessionEntry{}}}
}

func (h *Handler) Register(router *gin.Engine) {
	router.GET(stream.AgentPath, h.Serve)
}

func (h *Handler) Serve(c *gin.Context) {
	if h.gateway == nil || h.commands == nil {
		contract.Fail(c, contract.NewError("service_unavailable"))
		return
	}

	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || strings.TrimPrefix(values[0], "Bearer ") == "" {
		contract.Fail(c, contract.NewError("agent_unauthenticated"))
		return
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	identity, err := h.gateway.Authenticate(c.Request.Context(), token)
	if err != nil {
		contract.Fail(c, authError(err))
		return
	}
	if !validIdentity(identity) {
		contract.Fail(c, contract.NewError("agent_unauthenticated"))
		return
	}

	contract.SetPrincipal(c, contract.Principal{OrganizationID: contract.ID(identity.OrganizationID), TrapID: contract.ID(identity.TrapID), Role: contract.Agent})
	socket, err := stream.Upgrade(c, contract.AgentStream)
	if err != nil {
		return
	}
	defer socket.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	session := &agentSession{identity: identity, token: token, socket: socket, ctx: ctx, cancel: cancel, connectionID: string(contract.NewID())}
	if err := h.run(session); err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(c.Request.Context(), "agent stream failed", "request_id", c.Writer.Header().Get("X-Request-ID"), "trap_id", identity.TrapID, "error", err)

		// The close code is safe for the agent; causes are kept out of the wire.
		_ = socket.CloseCode(1011, "internal")
	}
}

func validIdentity(identity Identity) bool {
	return contract.ValidID(identity.OrganizationID) && contract.ValidID(identity.TrapID) && contract.ValidTypeID(identity.TypeID) && identity.TypeVersion > 0 && len(identity.RequiredActions) > 0
}

func sameIdentity(left, right Identity) bool {
	return left.OrganizationID == right.OrganizationID && left.TrapID == right.TrapID && left.TypeID == right.TypeID && left.TypeVersion == right.TypeVersion && slices.Equal(left.RequiredActions, right.RequiredActions)
}

func authError(err error) *contract.Error {
	var apiError *contract.Error
	if errors.As(err, &apiError) && (apiError.Code == "database_unavailable" || apiError.Code == "service_unavailable") {
		return apiError
	}

	return contract.NewError("agent_unauthenticated")
}

func (h *Handler) run(session *agentSession) (runErr error) {
	if err := session.socket.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("set hello deadline: %w", err)
	}

	first, err := session.socket.Read(session.ctx)
	if err != nil {
		if contract.ValidID(string(first.MessageID)) {
			_ = session.replyError(first, err)
		}
		_ = session.socket.CloseCode(4400, "invalid_hello")
		return nil
	}
	if first.Type != "agent.hello" {
		_ = session.socket.CloseCode(4400, "invalid_hello")
		return nil
	}

	var hello AgentHello
	if err := decodePayload(first.Payload, &hello); err != nil {
		_ = session.replyError(first, err)
		_ = session.socket.CloseCode(4400, "invalid_hello")
		return nil
	}
	if err := validateHello(session.identity, hello); err != nil {
		_ = session.replyError(first, err)
		_ = session.socket.CloseCode(4400, "invalid_hello")
		return nil
	}

	current, err := h.gateway.Authenticate(session.ctx, session.token)
	if err != nil || !sameIdentity(current, session.identity) {
		_ = session.socket.CloseCode(4401, "authentication_revoked")
		return nil
	}

	entry := h.registry.entry(session.identity.TrapID)
	entry.mu.Lock()
	if entry.current != nil {
		entry.current.active.Store(false)
		_ = entry.current.socket.CloseCode(4409, "session_replaced")
		entry.current.cancel()
	}
	entry.current = session
	session.active.Store(true)
	state, err := h.gateway.Observe(session.ctx, session.identity, session.connectionID, hello.Runtime)
	entry.mu.Unlock()
	if err != nil {
		_ = session.replyError(first, err)
		return h.disconnect(entry, session)
	}
	defer func() {
		runErr = errors.Join(runErr, h.disconnect(entry, session))
	}()
	session.heartbeatAt.Store(time.Now().UnixNano())

	if err := session.socket.Reply(session.ctx, first, "agent.welcome", map[string]any{
		"connection_id": session.connectionID, "trap_id": session.identity.TrapID,
		"server_time": time.Now().UTC(), "heartbeat_interval_seconds": interval(state),
		"offline_after_seconds": 30, "max_batch_events": 100, "max_message_bytes": contract.MaxBodyBytes,
		"telemetry_ack_timeout_seconds": 10, "current_configuration": state.CurrentConfiguration,
		"desired_profile_revision": state.DesiredProfileRevision, "desired_state": desiredState(state),
	}); err != nil {
		return fmt.Errorf("send welcome: %w", err)
	}

	return h.serveMessages(entry, session, hello)
}

func (h *Handler) disconnect(entry *sessionEntry, session *agentSession) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.current != session {
		return nil
	}
	entry.current = nil
	session.active.Store(false)
	session.cancel()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(session.ctx), 5*time.Second)
	defer cancel()
	if err := h.gateway.Offline(ctx, session.identity, session.connectionID); err != nil {
		return fmt.Errorf("mark agent offline: %w", err)
	}

	return nil
}

func interval(state State) int {
	if state.HeartbeatIntervalSeconds >= 5 && state.HeartbeatIntervalSeconds <= 10 {
		return state.HeartbeatIntervalSeconds
	}

	return 10
}

func desiredState(state State) string {
	if state.DesiredState == "running" {
		return "running"
	}

	return "stopped"
}

type agentSession struct {
	identity     Identity
	token        string
	socket       *stream.Socket
	ctx          context.Context
	cancel       context.CancelFunc
	connectionID string
	active       atomic.Bool
	heartbeatAt  atomic.Int64
}

type sessionEntry struct {
	mu      sync.Mutex
	current *agentSession
}

type sessionRegistry struct {
	mu      sync.Mutex
	entries map[string]*sessionEntry
}

func (r *sessionRegistry) entry(trapID string) *sessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[trapID]
	if entry == nil {
		entry = &sessionEntry{}
		r.entries[trapID] = entry
	}

	return entry
}

func (s *agentSession) replyError(request contract.Envelope, err error) error {
	apiError := wireError(err)
	payload := map[string]any{"error": map[string]any{"code": apiError.Code, "message": apiError.Message}}
	if apiError.Code == "ingestion_busy" {
		payload["retry_after_seconds"] = 1
	}
	if apiError.Code == "ingestion_pending" {
		payload["retry_after_seconds"] = 2
	}

	return s.socket.Reply(s.ctx, request, "error", payload)
}

func wireError(err error) *contract.Error {
	var apiError *contract.Error
	if errors.As(err, &apiError) {
		return apiError
	}

	for _, mapping := range []struct {
		err  error
		code string
	}{
		{commands.ErrStaleLease, "stale_command_lease"},
		{commands.ErrExpired, "command_expired"},
		{commands.ErrResultInvalid, "command_result_invalid"},
		{commands.ErrResultConflict, "command_result_conflict"},
		{commands.ErrUnavailable, "database_unavailable"},
	} {
		if errors.Is(err, mapping.err) {
			return contract.NewError(mapping.code)
		}
	}

	return contract.NewError("internal_error")
}
