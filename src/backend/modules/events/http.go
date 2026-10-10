package events

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	"honey-forge/internal/postgres"
	"honey-forge/internal/stream"
	authhttp "honey-forge/modules/auth/http"
)

type Handler struct {
	service *HistoryService
	cursors *contract.CursorCodec
	logger  *slog.Logger
}

func NewHandler(repository *Repository, cursors *contract.CursorCodec, logger *slog.Logger) *Handler {
	return &Handler{NewHistoryService(repository), cursors, logger}
}
func (h *Handler) RegisterRoutes(router *gin.Engine, session gin.HandlerFunc) {
	group := router.Group("/api/events", session, func(c *gin.Context) {
		a, ok := authhttp.Context(c)
		if !ok {
			contract.Fail(c, contract.NewError("unauthenticated"))
			return
		}
		contract.SetPrincipal(c, contract.Principal{UserID: contract.ID(a.UserID), OrganizationID: contract.ID(a.OrganizationID), Role: contract.Role(a.Role)})
		if _, err := readAccess(c.Request.Context()); err != nil {
			h.fail(c, err)
			return
		}
		c.Next()
	}, contract.RESTBody())
	group.GET("", h.list)
	group.GET("/:event_id", h.read)
}
func (h *Handler) read(c *gin.Context) {
	id, ok := contract.RequireID(c, "event_id")
	if !ok || !httpx.EmptyBody(c) {
		return
	}
	if _, ok := contract.RequestQuery(c); !ok {
		return
	}
	event, err := h.service.Read(c.Request.Context(), string(id))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, event)
}
func (h *Handler) list(c *gin.Context) {
	if !httpx.EmptyBody(c) {
		return
	}
	values, ok := contract.RequestQuery(c, "limit", "cursor", "trap_id", "from", "to", "source_ip", "event_type", "session_id")
	if !ok {
		return
	}
	parsed, err := contract.ParseListQuery(values, "trap_id", "from", "to", "source_ip", "event_type", "session_id")
	if err != nil {
		contract.Fail(c, err)
		return
	}
	interval, err := contract.ParseTimeRange(values)
	if err != nil {
		contract.Fail(c, err)
		return
	}
	q := Query{Limit: parsed.Limit, TrapID: strings.ToLower(values.Get("trap_id")), SessionID: strings.ToLower(values.Get("session_id")), EventType: values.Get("event_type"), From: interval.From, To: interval.To, Boundary: -1}
	for _, key := range []string{"trap_id", "session_id"} {
		if v, ok := values[key]; ok && !contract.ValidID(v[0]) {
			contract.Fail(c, contract.NewError("invalid_query"))
			return
		}
	}
	if v, ok := values["event_type"]; ok && !contract.ValidTypeID(v[0]) {
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}
	if v, ok := values["source_ip"]; ok {
		ip, err := netip.ParseAddr(v[0])
		if err != nil || ip.Zone() != "" {
			contract.Fail(c, contract.NewError("invalid_query"))
			return
		}
		q.SourceIP = ip.Unmap().String()
	}
	values.Del("cursor")
	values.Del("limit")
	values.Set("trap_id", q.TrapID)
	values.Set("session_id", q.SessionID)
	values.Set("source_ip", q.SourceIP)
	normalized := values.Encode()
	p, _ := contract.PrincipalFrom(c.Request.Context())
	scope := contract.CursorScope{OrganizationID: p.OrganizationID, Collection: "events", Filters: normalized}
	var streamCursor string
	if parsed.Cursor != "" {
		pos, err := h.cursors.Decode(scope, parsed.Cursor)
		parts := strings.SplitN(pos.Boundary, "/", 2)
		if err != nil || len(parts) != 2 {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}
		q.Boundary, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || q.Boundary < 0 {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}
		streamCursor = parts[1]
		after := strings.SplitN(pos.After, "/", 2)
		if len(after) != 2 || !contract.ValidID(after[1]) {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}
		at, err := time.Parse(time.RFC3339Nano, after[0])
		if err != nil {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}
		q.AfterTime = &at
		q.AfterID = after[1]
	}
	items, more, boundary, streamSequence, readErr := h.service.List(c.Request.Context(), q)
	if readErr != nil {
		h.fail(c, readErr)
		return
	}
	if streamCursor == "" {
		streamCursor, readErr = stream.EncodeCursor(h.cursors, p.OrganizationID, streamSequence, time.Now())
		if readErr != nil {
			h.fail(c, readErr)
			return
		}
	}
	var next *string
	if more {
		last := items[len(items)-1]
		cursor, err := h.cursors.Encode(scope, contract.CursorPosition{Boundary: fmt.Sprint(boundary) + "/" + streamCursor, After: last.OccurredAt + "/" + last.EventID})
		if err != nil {
			h.fail(c, err)
			return
		}
		next = &cursor
	}
	c.JSON(200, EventPage{Items: items, NextCursor: next, StreamCursor: streamCursor})
}
func (h *Handler) fail(c *gin.Context, err error) {
	var api *contract.Error
	if !errors.As(err, &api) {
		api = contract.NewError("internal_error")
		if postgres.IsUnavailable(err) {
			api = contract.NewError("database_unavailable")
		}
	}
	if api.Status >= 500 {
		h.logger.ErrorContext(c.Request.Context(), "event request failed", "error_code", api.Code, "error_type", fmt.Sprintf("%T", err))
	}
	contract.Fail(c, api)
}
