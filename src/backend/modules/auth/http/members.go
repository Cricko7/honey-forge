package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	"honey-forge/modules/auth"
)

func (h *Handler) members(c *gin.Context) {
	actor := resolvedSession(c).AuthContext()
	if actor.Role != auth.RoleAdmin {
		h.fail(c, "member list forbidden", auth.ErrForbidden)
		return
	}
	query, ok := contract.RequestQuery(c, "limit", "cursor")
	if !ok || !emptyBody(c) {
		return
	}
	parsed, queryErr := contract.ParseListQuery(query)
	if queryErr != nil {
		contract.Fail(c, queryErr)
		return
	}
	cursor := parsed.Cursor
	if _, exists := query["cursor"]; exists && !contract.ValidID(cursor) {
		httpx.WriteError(c, 400, "invalid_query", "Invalid query parameters")
		return
	}
	page, err := h.service.Members(c.Request.Context(), actor, parsed.Limit, cursor)
	if err != nil {
		h.fail(c, "member list failed", err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) setTrapPermission(c *gin.Context) {
	actor := resolvedSession(c).AuthContext()
	if actor.Role != auth.RoleAdmin {
		h.fail(c, "trap permission change forbidden", auth.ErrForbidden)
		return
	}
	id, ok := contract.RequireID(c, "id")
	if !ok {
		return
	}
	var req auth.TrapPermissionRequest
	if !bindRequest(c, &req) {
		return
	}
	if err := h.service.SetTrapPermission(c.Request.Context(), actor, string(id), *req.CanManageTraps, req.ExpectedRevision); err != nil {
		h.fail(c, "trap permission change failed", err)
		return
	}
	h.logger.InfoContext(c.Request.Context(), "member trap permission changed", "request_id", c.GetString("request_id"),
		"actor_user_id", actor.UserID, "organization_id", actor.OrganizationID, "target_user_id", string(id), "granted", *req.CanManageTraps)
	c.Status(http.StatusNoContent)
}

func (h *Handler) presence(c *gin.Context) {
	if !emptyBody(c) {
		return
	}
	if err := h.service.TouchPresence(c.Request.Context(), resolvedSession(c)); err != nil {
		h.fail(c, "presence failed", err)
		return
	}
	c.Status(http.StatusNoContent)
}
