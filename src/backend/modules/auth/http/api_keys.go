package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	authcore "honey-forge/modules/auth"
)

func (h *Handler) createAPIKey(c *gin.Context) {
	actor, _ := Context(c)
	if actor.Role != authcore.RoleAdmin {
		h.fail(c, "API key creation forbidden", authcore.ErrForbidden)
		return
	}

	var request authcore.CreateAPIKeyRequest
	if !bindRequest(c, &request) {
		return
	}

	key, raw, err := h.service.CreateAPIKey(c.Request.Context(), actor, request.Name)
	if err != nil {
		h.fail(c, "API key creation failed", err)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Header("Location", "/api/api-keys/"+key.ID)
	c.JSON(http.StatusCreated, struct {
		authcore.APIKey
		Token string `json:"token"`
	}{key, raw})
}

func (h *Handler) listAPIKeys(c *gin.Context) {
	actor, _ := Context(c)
	keys, err := h.service.ListAPIKeys(c.Request.Context(), actor)
	if err != nil {
		h.fail(c, "API key listing failed", err)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": keys})
}

func (h *Handler) revokeAPIKey(c *gin.Context) {
	actor, _ := Context(c)
	if actor.Role != authcore.RoleAdmin {
		h.fail(c, "API key revocation forbidden", authcore.ErrForbidden)
		return
	}

	if !contract.ValidID(c.Param("id")) {
		httpx.WriteError(c, http.StatusBadRequest, "invalid_id", "Invalid API key ID")
		return
	}

	if !emptyBody(c) {
		return
	}

	if err := h.service.RevokeAPIKey(c.Request.Context(), actor, c.Param("id")); err != nil {
		h.fail(c, "API key revocation failed", err)
		return
	}

	c.Status(http.StatusNoContent)
}
