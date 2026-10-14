package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"honey-forge/modules/auth"
)

func (h *Handler) requireAdminAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := h.service.RequireAdminAccess(c.Request.Context(), resolvedSession(c).AuthContext()); err != nil {
			h.fail(c, "admin confirmation required", err)
			return
		}
		c.Next()
	}
}

func (h *Handler) adminAccess(c *gin.Context) {
	if !emptyBody(c) {
		return
	}
	view, err := h.service.AdminAccess(c.Request.Context(), resolvedSession(c).AuthContext())
	if err != nil {
		h.fail(c, "admin confirmation status failed", err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handler) confirmAdminAccess(c *gin.Context) {
	sess := resolvedSession(c)
	if sess.View.User.Role != auth.RoleAdmin {
		h.fail(c, "admin confirmation forbidden", auth.ErrForbidden)
		return
	}
	var req auth.AdminPasswordRequest
	if !bindRequest(c, &req) {
		return
	}
	view, err := h.service.ConfirmAdminAccess(c.Request.Context(), sess, req.Password)
	if err != nil {
		h.fail(c, "admin password confirmation failed", err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handler) lockAdminAccess(c *gin.Context) {
	if !emptyBody(c) {
		return
	}
	if err := h.service.LockAdminAccess(c.Request.Context(), resolvedSession(c).AuthContext()); err != nil {
		h.fail(c, "admin panel lock failed", err)
		return
	}
	c.Status(http.StatusNoContent)
}
