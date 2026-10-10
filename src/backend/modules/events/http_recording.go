package events

import (
	"strings"

	"github.com/gin-gonic/gin"
	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
)

func (h *Handler) recording(c *gin.Context) {
	id, ok := contract.RequireID(c, "session_id")
	if !ok || !httpx.EmptyBody(c) {
		return
	}
	values, ok := contract.RequestQuery(c, "trap_id")
	if !ok {
		return
	}
	trapID := strings.ToLower(values.Get("trap_id"))
	if !contract.ValidID(trapID) {
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}
	result, err := h.recordings.Read(c.Request.Context(), trapID, string(id))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, result)
}
