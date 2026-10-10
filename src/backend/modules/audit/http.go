package audit

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	"honey-forge/internal/postgres"
	authhttp "honey-forge/modules/auth/http"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Handler struct {
	service *Service
	cursors *contract.CursorCodec
	logger  *slog.Logger
}

func NewHandler(s *Service, c *contract.CursorCodec, l *slog.Logger) *Handler {
	return &Handler{s, c, l}
}
func (h *Handler) RegisterRoutes(r *gin.Engine, session gin.HandlerFunc) {
	g := r.Group("/api/audit-entries", session, func(c *gin.Context) {
		a, ok := authhttp.Context(c)
		if !ok {
			contract.Fail(c, contract.NewError("unauthenticated"))
			return
		}
		contract.SetPrincipal(c, contract.Principal{UserID: contract.ID(a.UserID), OrganizationID: contract.ID(a.OrganizationID), Role: contract.Role(a.Role)})
		c.Next()
	}, contract.RESTBody())
	g.GET("", h.list)
	g.GET("/:audit_id", h.read)
}
func (h *Handler) read(c *gin.Context) {
	id, ok := contract.RequireID(c, "audit_id")
	if !ok || !httpx.EmptyBody(c) {
		return
	}
	if _, ok := contract.RequestQuery(c); !ok {
		return
	}
	e, err := h.service.Read(c.Request.Context(), string(id))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, e)
}
func (h *Handler) list(c *gin.Context) {
	if !httpx.EmptyBody(c) {
		return
	}
	values, ok := contract.RequestQuery(c, "limit", "cursor", "from", "to", "action", "actor_id", "resource_id")
	if !ok {
		return
	}
	parsed, err := contract.ParseListQuery(values, "from", "to", "action", "actor_id", "resource_id")
	if err != nil {
		contract.Fail(c, err)
		return
	}
	interval, err := contract.ParseTimeRange(values)
	if err != nil {
		contract.Fail(c, err)
		return
	}
	for _, key := range []string{"actor_id", "resource_id"} {
		if v, ok := values[key]; ok {
			if !contract.ValidID(v[0]) {
				contract.Fail(c, contract.NewError("invalid_query"))
				return
			}
			values.Set(key, strings.ToLower(v[0]))
		}
	}
	if v, ok := values["action"]; ok && (utf8.RuneCountInString(v[0]) < 1 || utf8.RuneCountInString(v[0]) > 100) {
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}
	for key, at := range map[string]*time.Time{"from": interval.From, "to": interval.To} {
		if at != nil {
			values.Set(key, at.UTC().Format(time.RFC3339Nano))
		}
	}
	q := Query{Limit: parsed.Limit, Action: values.Get("action"), ActorID: values.Get("actor_id"), ResourceID: values.Get("resource_id"), From: interval.From, To: interval.To, Boundary: -1}
	values.Del("limit")
	values.Del("cursor")
	p, _ := contract.PrincipalFrom(c.Request.Context())
	scope := contract.CursorScope{OrganizationID: p.OrganizationID, Collection: "audit-entries", Filters: values.Encode()}
	if parsed.Cursor != "" {
		pos, err := h.cursors.Decode(scope, parsed.Cursor)
		parts := strings.SplitN(pos.After, "/", 2)
		if err != nil || len(parts) != 2 || !contract.ValidID(parts[1]) {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}
		q.Boundary, err = strconv.ParseInt(pos.Boundary, 10, 64)
		if err != nil || q.Boundary < 0 {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}
		after, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}
		q.AfterTime = &after
		q.AfterID = parts[1]
	}
	items, more, boundary, listErr := h.service.List(c.Request.Context(), q)
	if listErr != nil {
		h.fail(c, listErr)
		return
	}
	var next *string
	if more {
		last := items[len(items)-1]
		cursor, err := h.cursors.Encode(scope, contract.CursorPosition{Boundary: fmt.Sprint(boundary), After: last.OccurredAt.Format(time.RFC3339Nano) + "/" + last.ID})
		if err != nil {
			h.fail(c, err)
			return
		}
		next = &cursor
	}
	c.JSON(200, contract.NewPage(items, next))
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
		h.logger.ErrorContext(c.Request.Context(), "audit request failed", "error_code", api.Code, "error_type", fmt.Sprintf("%T", err))
	}
	contract.Fail(c, api)
}
