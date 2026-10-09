package http

import (
	authhttp "honey-forge/src/backend/modules/auth/http"
	profilecore "honey-forge/src/backend/modules/profiles"
	profileservice "honey-forge/src/backend/modules/profiles/service"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"honey-forge/src/backend/internal/platform/httpx"
)

type Handler struct {
	service *profileservice.Service
	logger  *slog.Logger
}

func NewHandler(service *profileservice.Service, logger *slog.Logger) *Handler {
	return &Handler{service, logger}
}

func (h *Handler) RegisterRoutes(r *gin.Engine, session gin.HandlerFunc) error {
	if err := profilecore.RegisterValidation(); err != nil {
		return err
	}

	group := r.Group("/api/profiles", session, h.requireRole, httpx.BodyLimit)
	group.POST("", h.create)
	group.GET("", h.list)
	group.GET("/:id", h.read)
	group.PATCH("/:id", h.patch)
	group.DELETE("/:id", h.delete)
	return nil
}

func (h *Handler) requireRole(c *gin.Context) {
	a, _ := authhttp.Context(c)
	if err := profileservice.Authorize(a, c.Request.Method != http.MethodGet); err != nil {
		h.fail(c, err)
		return
	}

	c.Next()
}

func (h *Handler) create(c *gin.Context) {
	if !noQuery(c) {
		return
	}

	var req profilecore.CreateRequest
	if !httpx.BindJSON(c, &req, nil) {
		return
	}

	a, _ := authhttp.Context(c)
	result, err := h.service.Create(c.Request.Context(), a, req)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.Header("Location", "/api/profiles/"+result.Profile.ID)
	if result.Current {
		c.Header("ETag", profilecore.ETag(result.Profile))
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
		c.Header("Idempotency-Replayed", "true")
	}

	c.JSON(status, result.Profile)
}

func (h *Handler) read(c *gin.Context) {
	id, ok := profileID(c)
	if !ok || !noQuery(c) || !httpx.EmptyBody(c) {
		return
	}

	a, _ := authhttp.Context(c)
	p, err := h.service.ReadProfile(c.Request.Context(), a, id)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.Header("ETag", profilecore.ETag(p))
	c.JSON(http.StatusOK, p)
}

func (h *Handler) patch(c *gin.Context) {
	id, ok := profileID(c)
	if !ok || !noQuery(c) {
		return
	}

	a, _ := authhttp.Context(c)
	if _, err := h.service.ReadProfile(c.Request.Context(), a, id); err != nil {
		h.fail(c, err)
		return
	}

	var req profilecore.PatchRequest
	if !httpx.BindJSON(c, &req, nil) {
		return
	}

	match, ok := matchHeader(c)
	if !ok {
		return
	}

	p, err := h.service.Patch(c.Request.Context(), a, id, match, req)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.Header("ETag", profilecore.ETag(p))
	c.JSON(http.StatusOK, p)
}

func (h *Handler) delete(c *gin.Context) {
	id, ok := profileID(c)
	if !ok || !noQuery(c) {
		return
	}

	a, _ := authhttp.Context(c)
	if _, err := h.service.ReadProfile(c.Request.Context(), a, id); err != nil {
		h.fail(c, err)
		return
	}

	if !httpx.EmptyBody(c) {
		return
	}

	match, ok := matchHeader(c)
	if !ok {
		return
	}

	if err := h.service.Delete(c.Request.Context(), a, id, match); err != nil {
		h.fail(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func profileID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if !profilecore.ValidID(id) {
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

func matchHeader(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("If-Match")
	if len(values) > 1 {
		httpx.WriteError(c, 400, "invalid_precondition", "Invalid precondition header")
		return "", false
	}

	return c.GetHeader("If-Match"), true
}
