package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"honey-forge/internal/contract"
)

func (h *Handler) removeMember(c *gin.Context) {
	id, ok := contract.RequireID(c, "id")
	if !ok || !emptyBody(c) {
		return
	}
	actor := resolvedSession(c).AuthContext()
	if err := h.service.RemoveMember(c.Request.Context(), actor, string(id)); err != nil {
		h.fail(c, "member removal failed", err)
		return
	}
	h.logger.InfoContext(c.Request.Context(), "organization member removed", "request_id", c.GetString("request_id"),
		"actor_user_id", actor.UserID, "organization_id", actor.OrganizationID, "target_user_id", string(id))
	c.Status(http.StatusNoContent)
}
