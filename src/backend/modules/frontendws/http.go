package frontendws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/postgres"
	"honey-forge/internal/stream"
	"honey-forge/modules/auth"
)

var errSlowConsumer = errors.New("slow_consumer")

type Handler struct {
	journal   journal
	sessions  sessions
	cursors   *contract.CursorCodec
	browser   *contract.BrowserPolicy
	logger    *slog.Logger
	shutdown  context.Context
	lifecycle sync.Mutex // serializes registration with shutdown waiting
	active    sync.WaitGroup
}

func NewHandler(journal journal, sessions sessions, cursors *contract.CursorCodec, browser *contract.BrowserPolicy, logger *slog.Logger, shutdown context.Context) *Handler {
	return &Handler{journal: journal, sessions: sessions, cursors: cursors, browser: browser, logger: logger, shutdown: shutdown}
}
func (h *Handler) Register(router *gin.Engine) { router.GET(stream.FrontendPath, h.serve) }

// Wait is called after cancelling shutdown, before closing database dependencies.
func (h *Handler) Wait() { h.lifecycle.Lock(); h.lifecycle.Unlock(); h.active.Wait() }

func (h *Handler) authenticate(ctx context.Context, cookie string) (contract.Principal, error) {
	sess, err := h.sessions.ResolveSession(ctx, cookie)
	if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrNotFound) {
		return contract.Principal{}, contract.NewError("unauthenticated")
	}
	if err != nil {
		return contract.Principal{}, err
	}
	a := sess.AuthContext()
	p := contract.Principal{UserID: contract.ID(a.UserID), OrganizationID: contract.ID(a.OrganizationID), Role: contract.Role(a.Role)}
	if err := contract.AuthorizeCapability(contract.WithPrincipal(ctx, p), contract.FrontendStream); err != nil {
		return p, err
	}
	return p, nil
}

func (h *Handler) serve(c *gin.Context) {
	h.lifecycle.Lock()
	if h.shutdown.Err() != nil {
		h.lifecycle.Unlock()
		contract.Fail(c, contract.NewError("service_unavailable"))
		return
	}
	h.active.Add(1)
	h.lifecycle.Unlock()
	defer h.active.Done()
	cookies := c.Request.CookiesNamed("__Host-session")
	if len(cookies) != 1 {
		contract.Fail(c, contract.NewError("unauthenticated"))
		return
	}
	cookie := cookies[0].Value
	work, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	p, err := h.authenticate(work, cookie)
	cancel()
	if err != nil {
		h.httpFailure(c, err)
		return
	}
	contract.SetPrincipal(c, p)
	// Authenticate and check exact Origin/protocol before touching the journal.
	if !h.browser.CheckOrigin(c) {
		return
	}
	protocols := c.Request.Header.Values("Sec-WebSocket-Protocol")
	if len(protocols) != 1 || protocols[0] != "dashboard-stream.v1" {
		contract.Fail(c, contract.NewError("invalid_ws_protocol"))
		return
	}
	work, cancel = context.WithTimeout(c.Request.Context(), 5*time.Second)
	_, _, err = h.journal.Boundary(work)
	cancel()
	if err != nil {
		h.httpFailure(c, err)
		return
	}
	if h.shutdown.Err() != nil {
		contract.Fail(c, contract.NewError("service_unavailable"))
		return
	}
	socket, err := stream.Upgrade(c, contract.FrontendStream, h.browser)
	if err != nil {
		return
	}
	defer func() {
		if err := socket.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			h.log(err)
		}
	}()
	ctx, stop := context.WithCancel(c.Request.Context())
	defer stop()
	var once sync.Once
	closeConnection := func(code int, reason string) {
		once.Do(func() {
			if err := socket.CloseCode(code, reason); err != nil {
				h.log(err)
			}
			stop()
		})
	}
	stopShutdown := context.AfterFunc(h.shutdown, func() { closeConnection(1013, "service_unavailable") })
	defer stopShutdown()
	if err := socket.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		h.log(err)
		return
	}
	request, err := socket.Read(ctx)
	if err != nil {
		var api *contract.Error
		var timeout net.Error
		if errors.As(err, &api) {
			h.protocolFailure(ctx, socket, request, "invalid_message", closeConnection)
		} else if errors.As(err, &timeout) && timeout.Timeout() {
			closeConnection(4408, "timeout")
		}
		return
	}
	after, err := subscribe(request)
	if err != nil {
		h.protocolFailure(ctx, socket, request, "invalid_message", closeConnection)
		return
	}
	work, cancel = context.WithTimeout(ctx, 5*time.Second)
	boundary, floor, err := h.journal.Boundary(work)
	cancel()
	if err != nil {
		h.subscribeFailure(ctx, socket, request, err, closeConnection)
		return
	}
	sequence := boundary
	if after != nil {
		sequence, err = stream.DecodeCursor(h.cursors, p.OrganizationID, *after, time.Now())
		if err == nil && sequence > boundary {
			err = contract.NewError("invalid_cursor")
		}
		if err == nil && sequence < floor {
			err = contract.NewError("cursor_expired")
		}
		if err != nil {
			var api *contract.Error
			if errors.As(err, &api) {
				h.protocolFailure(ctx, socket, request, api.Code, closeConnection)
			} else {
				h.streamFailure(err, closeConnection)
			}
			return
		}
	}
	h.connected(ctx, socket, cookie, p, request, sequence, boundary, after != nil, closeConnection)
}

func (h *Handler) subscribeFailure(ctx context.Context, socket *stream.Socket, request contract.Envelope, cause error, closeConnection func(int, string)) {
	if ctx.Err() != nil {
		return
	}
	code, closeCode := "internal_error", 1011
	payload := map[string]any{}
	if postgres.IsUnavailable(cause) || errors.Is(cause, auth.ErrUnavailable) {
		code, closeCode = "service_unavailable", 1013
		payload["retry_after_seconds"] = 1
	}
	payload["error"] = contract.NewError(code)
	e, _, err := envelope(&request.MessageID, "error", payload)
	if err == nil {
		err = socket.Write(ctx, e)
	}
	if err != nil {
		h.log(err)
	} else {
		h.log(cause)
	}
	closeConnection(closeCode, code)
}

func (h *Handler) httpFailure(c *gin.Context, err error) {
	var api *contract.Error
	if !errors.As(err, &api) {
		api = contract.NewError("internal_error")
		if postgres.IsUnavailable(err) || errors.Is(err, auth.ErrUnavailable) {
			api = contract.NewError("database_unavailable")
		}
		h.log(err)
	}
	contract.Fail(c, api)
}
func (h *Handler) protocolFailure(ctx context.Context, s *stream.Socket, request contract.Envelope, code string, closeConnection func(int, string)) {
	var reply *contract.ID
	if contract.ValidID(string(request.MessageID)) {
		reply = &request.MessageID
	}
	e, _, err := envelope(reply, "error", map[string]any{"error": contract.NewError(code)})
	if err == nil {
		err = s.Write(ctx, e)
	}
	if err != nil {
		h.log(err)
	}
	closeConnection(4400, code)
}
func (h *Handler) streamFailure(err error, closeConnection func(int, string)) {
	switch {
	case errors.Is(err, errSlowConsumer):
		closeConnection(4410, "slow_consumer")
	case errors.Is(err, auth.ErrUnavailable), postgres.IsUnavailable(err):
		h.log(err)
		closeConnection(1013, "service_unavailable")
	default:
		var api *contract.Error
		if errors.As(err, &api) && api.Code == "unauthenticated" {
			closeConnection(4401, "unauthenticated")
			return
		}
		if errors.As(err, &api) && api.Code == "forbidden" {
			closeConnection(4403, "forbidden")
			return
		}
		if !errors.Is(err, context.Canceled) {
			h.log(err)
			closeConnection(1011, "internal_error")
		}
	}
}
func (h *Handler) log(err error) {
	h.logger.Error("frontend stream failed", "error_type", fmt.Sprintf("%T", err))
}
