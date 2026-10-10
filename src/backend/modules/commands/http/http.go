package http

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	authhttp "honey-forge/modules/auth/http"
	"honey-forge/modules/commands"
	"honey-forge/modules/commands/service"
)

type Handler struct {
	service *service.Service
	cursors *contract.CursorCodec
	logger  *slog.Logger
}

func NewHandler(s *service.Service, cursors *contract.CursorCodec, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{service: s, cursors: cursors, logger: logger}
}

func (h *Handler) RegisterRoutes(r *gin.Engine, session gin.HandlerFunc) {
	r.POST("/api/traps/:id/commands", session, h.authorize, httpx.BodyLimit, h.create)
	r.GET("/api/traps/:id/commands", session, h.authorize, httpx.BodyLimit, h.list)
	r.GET("/api/traps/:id/commands/:command_id", session, h.authorize, httpx.BodyLimit, h.read)
}

func (h *Handler) authorize(c *gin.Context) {
	a, _ := authhttp.Context(c)
	if err := service.Authorize(a, c.Request.Method != http.MethodGet); err != nil {
		h.fail(c, err)
		return
	}

	c.Next()
}

func (h *Handler) create(c *gin.Context) {
	if !noQuery(c) {
		return
	}

	id, ok := trapID(c)
	if !ok {
		return
	}

	var req commands.CreateRequest
	if !httpx.BindJSON(c, &req, nil) {
		return
	}

	a, _ := authhttp.Context(c)
	result, err := h.service.Create(c.Request.Context(), a, id, req)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.Header("Location", "/api/traps/"+id+"/commands/"+result.Command.ID)
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
		c.Header("Idempotency-Replayed", "true")
	}

	c.JSON(status, result.Command)
}

func (h *Handler) read(c *gin.Context) {
	id, ok := trapID(c)
	if !ok || !noQuery(c) || !httpx.EmptyBody(c) {
		return
	}

	commandID := c.Param("command_id")
	if !contract.ValidID(commandID) {
		httpx.WriteError(c, 400, "invalid_id", "Invalid resource identifier")
		return
	}

	a, _ := authhttp.Context(c)
	command, err := h.service.Read(c.Request.Context(), a, id, commandID)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, command)
}

func (h *Handler) list(c *gin.Context) {
	id, ok := trapID(c)
	if !ok || !httpx.EmptyBody(c) {
		return
	}

	values, ok := contract.RequestQuery(c, "limit", "cursor", "status")
	if !ok {
		return
	}

	list, queryErr := contract.ParseListQuery(values, "status")
	if queryErr != nil {
		contract.Fail(c, queryErr)
		return
	}

	status := commands.Status(values.Get("status"))
	switch status {
	case "", commands.Queued, commands.Running, commands.Succeeded, commands.Failed, commands.Expired:
	default:
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}

	if _, provided := values["status"]; provided && status == "" {
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}

	a, _ := authhttp.Context(c)
	query := commands.ListQuery{Limit: list.Limit, Status: status}
	scope := contract.CursorScope{OrganizationID: contract.ID(a.OrganizationID), Collection: "trap-commands:" + id, Filters: string(status)}
	if list.Cursor != "" {
		if h.cursors == nil {
			h.fail(c, commands.ErrUnavailable)
			return
		}

		position, err := h.cursors.Decode(scope, list.Cursor)
		parts := strings.SplitN(position.After, "/", 2)
		if err != nil || position.Boundary != id || len(parts) != 2 || !contract.ValidID(parts[1]) {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}

		query.AfterTime, err = time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			contract.Fail(c, contract.NewError("invalid_cursor"))
			return
		}

		query.AfterID = parts[1]
	}

	items, more, err := h.service.List(c.Request.Context(), a, id, query)
	if err != nil {
		h.fail(c, err)
		return
	}

	var next *string
	if more && len(items) > 0 {
		if h.cursors == nil {
			h.fail(c, commands.ErrUnavailable)
			return
		}

		last := items[len(items)-1]
		encoded, err := h.cursors.Encode(scope, contract.CursorPosition{Boundary: id, After: last.CreatedAt.Format(time.RFC3339Nano) + "/" + last.ID})
		if err != nil {
			h.fail(c, err)
			return
		}

		next = &encoded
	}

	c.JSON(http.StatusOK, contract.NewPage(items, next))
}

func trapID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if !contract.ValidID(id) {
		httpx.WriteError(c, 400, "invalid_id", "Invalid resource identifier")
		return "", false
	}

	return strings.ToLower(id), true
}

func noQuery(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" {
		httpx.WriteError(c, 400, "invalid_query", "Invalid query parameters")
		return false
	}

	return true
}

func (h *Handler) fail(c *gin.Context, err error) {
	status, code, message := 500, "internal_error", "Internal server error"
	for _, entry := range []struct {
		err           error
		status        int
		code, message string
	}{
		{commands.ErrUnauthorized, 401, "unauthenticated", "Authentication required"},
		{commands.ErrForbidden, 403, "forbidden", "Insufficient permissions"},
		{commands.ErrNotFound, 404, "resource_not_found", "Resource not found"},
		{commands.ErrValidation, 422, "validation_failed", "Request validation failed"},
		{commands.ErrInvalidParams, 422, "command_params_invalid", "Command parameters are invalid"},
		{commands.ErrUnsupportedAction, 422, "unsupported_action", "Action is not supported by this trap type"},
		{commands.ErrProfileChanged, 409, "profile_changed", "Profile revision has changed"},
		{commands.ErrConfigurationNotApplied, 409, "configuration_not_applied", "Trap configuration has not been applied"},
		{commands.ErrInProgress, 409, "command_in_progress", "Another command is in progress"},
		{commands.ErrIdempotencyConflict, 409, "idempotency_conflict", "Request identifier was used with different content"},
		{commands.ErrUnavailable, 503, "database_unavailable", "Database is temporarily unavailable"},
	} {
		if errors.Is(err, entry.err) {
			status, code, message = entry.status, entry.code, entry.message
			break
		}
	}

	var domain *contract.Error
	if errors.As(err, &domain) {
		status, code, message = domain.Status, domain.Code, domain.Message
	}

	level := slog.LevelWarn
	if status >= 500 {
		level = slog.LevelError
	}

	h.logger.Log(c.Request.Context(), level, "command request failed", "request_id", c.GetString("request_id"), "error_code", code, "error_type", fmt.Sprintf("%T", err))
	httpx.WriteError(c, status, code, message)
}
