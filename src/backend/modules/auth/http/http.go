package http

import (
	authcore "honey-forge/src/backend/modules/auth"
	authservice "honey-forge/src/backend/modules/auth/service"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"honey-forge/src/backend/internal/platform/httpx"
)

const (
	sessionCookie = "__Host-session"
	maxBodySize   = httpx.MaxBodySize
)

type Handler struct {
	service *authservice.Service
	logger  *slog.Logger
}

func NewHandler(service *authservice.Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) error {
	if err := authcore.RegisterValidationRules(); err != nil {
		return err
	}

	group := r.Group("/api", bodyLimit)
	limiter := newAuthLimiter()
	group.POST("/registrations", limiter.middleware, rejectQuery, h.register)
	group.POST("/sessions", limiter.middleware, rejectQuery, h.login)
	group.GET("/session", h.RequireSession(), rejectQuery, h.current)
	group.DELETE("/session", rejectQuery, h.logout)
	group.GET("/organization", h.RequireSession(), rejectQuery, h.organization)
	group.GET("/organization/join-code", h.RequireSession(), rejectQuery, h.joinCode)
	group.POST("/organization/join-code/rotations", h.RequireSession(), rejectQuery, h.rotateJoinCode)

	return nil
}

func (h *Handler) register(c *gin.Context) {
	if err := h.service.CheckRegistrationSession(c.Request.Context(), readSessionCookie(c)); err != nil {
		h.fail(c, "registration rejected", err)
		return
	}

	var req authcore.RegisterRequest
	if !bindRequest(c, &req) {
		return
	}

	view, raw, err := h.service.Register(c.Request.Context(), req, "")
	if err != nil {
		h.fail(c, "registration failed", err)
		return
	}

	h.createdSession(c, view, raw, "operator registered")
}

func (h *Handler) login(c *gin.Context) {
	var req authcore.LoginRequest
	if !bindRequest(c, &req) {
		return
	}

	view, raw, err := h.service.Login(c.Request.Context(), req, readSessionCookie(c))
	if err != nil {
		h.fail(c, "login failed", err)
		return
	}

	h.createdSession(c, view, raw, "operator logged in")
}

func (h *Handler) createdSession(c *gin.Context, view authcore.SessionView, raw, event string) {
	h.cookie(c, raw, int(authcore.SessionTTL.Seconds()), view.ExpiresAt)
	h.logger.InfoContext(c.Request.Context(), event,
		"request_id", c.GetString("request_id"),
		"user_id", view.User.ID,
		"organization_id", view.User.OrganizationID,
		"role", view.User.Role,
	)

	c.Header("Location", "/api/session")
	c.JSON(http.StatusCreated, view)
}

func (h *Handler) current(c *gin.Context) {
	sess := resolvedSession(c)
	c.JSON(http.StatusOK, sess.View)
}

func (h *Handler) logout(c *gin.Context) {
	if !emptyBody(c) {
		return
	}

	csrf := c.GetHeader("X-CSRF-Token")
	if len(c.Request.Header.Values("X-CSRF-Token")) != 1 {
		csrf = ""
	}

	if err := h.service.Logout(c.Request.Context(), readSessionCookie(c), csrf); err != nil {
		h.fail(c, "logout failed", err)
		return
	}

	h.cookie(c, "", -1, time.Unix(1, 0))
	h.logger.InfoContext(c.Request.Context(), "browser session revoked", "request_id", c.GetString("request_id"))
	c.Status(http.StatusNoContent)
}

func (h *Handler) organization(c *gin.Context) {
	org, err := h.service.Organization(c.Request.Context(), resolvedSession(c).AuthContext())
	if err != nil {
		h.fail(c, "organization lookup failed", err)
		return
	}

	c.JSON(http.StatusOK, org)
}

func (h *Handler) joinCode(c *gin.Context) {
	code, err := h.service.JoinCode(c.Request.Context(), resolvedSession(c).AuthContext())
	if err != nil {
		h.fail(c, "join code lookup failed", err)
		return
	}

	c.JSON(http.StatusOK, code)
}

func (h *Handler) rotateJoinCode(c *gin.Context) {
	actor := resolvedSession(c).AuthContext()
	if actor.Role != authcore.RoleAdmin {
		h.fail(c, "join code rotation forbidden", authcore.ErrForbidden)
		return
	}

	var req authcore.RotateJoinCodeRequest
	if !bindRequest(c, &req) {
		return
	}

	code, err := h.service.RotateJoinCode(c.Request.Context(), actor, int32(req.ExpectedRevision))
	if err != nil {
		h.fail(c, "join code rotation failed", err)
		return
	}

	h.logger.InfoContext(c.Request.Context(), "organization.join_code_rotated",
		"request_id", c.GetString("request_id"), "user_id", actor.UserID,
		"organization_id", actor.OrganizationID, "revision", code.Revision,
	)
	c.JSON(http.StatusOK, code)
}

func (h *Handler) cookie(c *gin.Context, raw string, maxAge int, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    raw,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  expires.UTC(),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}
