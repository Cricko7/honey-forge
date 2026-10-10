package traps

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	"honey-forge/internal/postgres"
	authhttp "honey-forge/modules/auth/http"
)

type Handler struct {
	service *Service
	cursors *contract.CursorCodec
	logger  *slog.Logger
}

func NewHandler(s *Service, cursors *contract.CursorCodec, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{s, cursors, logger}
}
func (h *Handler) RegisterRoutes(router *gin.Engine, session gin.HandlerFunc) {
	group := router.Group("/api/traps", session, h.authorize, contract.RESTBody())
	group.POST("", h.create)
	group.GET("", h.list)
	group.GET("/:id", h.read)
	group.PATCH("/:id", h.patch)
	group.DELETE("/:id", h.delete)
	group.GET("/:id/agent-credentials", h.credentials)
	group.POST("/:id/agent-credentials", h.issueCredentials)
	group.DELETE("/:id/agent-credentials", h.revokeCredentials)
}
func (h *Handler) authorize(c *gin.Context) {
	if a, ok := authhttp.Context(c); ok {
		contract.SetPrincipal(c, contract.Principal{UserID: contract.ID(a.UserID), OrganizationID: contract.ID(a.OrganizationID), Role: contract.Role(a.Role)})
	}
	admin := c.Request.Method != http.MethodGet || strings.HasSuffix(c.FullPath(), "/agent-credentials")
	if _, err := access(c.Request.Context(), admin); err != nil {
		h.fail(c, err)
		return
	}
	c.Next()
}
func noQuery(c *gin.Context) bool { _, ok := contract.RequestQuery(c); return ok }
func (h *Handler) target(c *gin.Context) (string, bool) {
	id, ok := contract.RequireID(c, "id")
	if !ok || !noQuery(c) {
		return "", false
	}
	// Ownership precedes payload/precondition errors on an individual resource.
	if _, err := h.service.Read(c.Request.Context(), string(id)); err != nil {
		h.fail(c, err)
		return "", false
	}
	return string(id), true
}
func (h *Handler) create(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	var req CreateRequest
	if !httpx.BindJSON(c, &req, nil) {
		return
	}
	result, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Location", "/api/traps/"+result.Trap.ID)
	status := 201
	if result.Replayed {
		status = 200
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(status, result.Trap)
}
func (h *Handler) read(c *gin.Context) {
	id, ok := contract.RequireID(c, "id")
	if !ok || !noQuery(c) || !httpx.EmptyBody(c) {
		return
	}
	trap, err := h.service.Read(c.Request.Context(), string(id))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, trap)
}
func (h *Handler) patch(c *gin.Context) {
	id, ok := h.target(c)
	if !ok {
		return
	}
	var req PatchRequest
	if !httpx.BindJSON(c, &req, nil) {
		return
	}
	revision, ok := contract.ExpectedTrapRevision(c)
	if !ok {
		return
	}
	trap, err := h.service.Patch(c.Request.Context(), id, int64(revision), req)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, trap)
}
func (h *Handler) delete(c *gin.Context) {
	id, ok := h.target(c)
	if !ok || !httpx.EmptyBody(c) {
		return
	}
	revision, ok := contract.ExpectedTrapRevision(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id, int64(revision)); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(204)
}
func (h *Handler) credentials(c *gin.Context) {
	id, ok := h.target(c)
	if !ok || !httpx.EmptyBody(c) {
		return
	}
	status, err := h.service.Credentials(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("ETag", CredentialsETag(id, status.Generation))
	c.JSON(200, status)
}
func (h *Handler) issueCredentials(c *gin.Context) {
	id, ok := h.target(c)
	if !ok {
		return
	}
	var req CredentialsRequest
	if !httpx.BindJSON(c, &req, nil) {
		return
	}
	credentials, err := h.service.IssueCredentials(c.Request.Context(), id, *req.ExpectedGeneration)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("ETag", CredentialsETag(id, credentials.Generation))
	c.JSON(200, credentials)
}
func (h *Handler) revokeCredentials(c *gin.Context) {
	id, ok := h.target(c)
	if !ok || !httpx.EmptyBody(c) {
		return
	}
	values := c.Request.Header.Values("If-Match")
	if len(values) == 0 {
		contract.Fail(c, contract.NewError("precondition_required"))
		return
	}
	if len(values) != 1 || !credentialTag(values[0]) {
		contract.Fail(c, contract.NewError("invalid_precondition"))
		return
	}
	if err := h.service.RevokeCredentials(c.Request.Context(), id, values[0]); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(204)
}
func credentialTag(tag string) bool {
	if !strings.HasPrefix(tag, `"agent-credentials:`) || !strings.HasSuffix(tag, `"`) {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(tag, `"agent-credentials:`), `"`), ":")
	if len(parts) != 2 || !contract.ValidID(parts[0]) {
		return false
	}
	n, err := strconv.ParseInt(parts[1], 10, 32)
	return err == nil && n >= 0 && parts[1] == strconv.FormatInt(n, 10)
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
		h.logger.ErrorContext(c.Request.Context(), "trap request failed", "request_id", c.Writer.Header().Get("X-Request-ID"), "error_code", api.Code, "error_type", fmt.Sprintf("%T", err))
	}
	contract.Fail(c, api)
}
