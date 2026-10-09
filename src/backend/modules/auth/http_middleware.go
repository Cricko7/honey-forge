package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
)

// RequireSession passes a verified identity to other operator feature handlers.
func (h *Handler) RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		sess, err := h.service.ResolveSession(c.Request.Context(), readSessionCookie(c))
		if err != nil {
			h.fail(c, "session authentication failed", err)
			return
		}

		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			if len(c.Request.Header.Values("X-CSRF-Token")) != 1 {
				h.fail(c, "CSRF rejected", ErrCSRF)
				return
			}
			if err := CheckCSRF(sess, c.GetHeader("X-CSRF-Token")); err != nil {
				h.fail(c, "CSRF rejected", err)
				return
			}
		}

		c.Set("resolved_session", sess)
		c.Set("auth_context", sess.AuthContext())
		c.Next()
	}
}

func Context(c *gin.Context) (AuthContext, bool) {
	value, ok := c.Get("auth_context")
	if !ok {
		return AuthContext{}, false
	}

	actor, ok := value.(AuthContext)

	return actor, ok
}

func resolvedSession(c *gin.Context) ResolvedSession {
	return c.MustGet("resolved_session").(ResolvedSession)
}

func readSessionCookie(c *gin.Context) string {
	cookies := c.Request.CookiesNamed(sessionCookie)
	if len(cookies) != 1 {
		return ""
	}

	return cookies[0].Value
}

func rejectQuery(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		httpx.WriteError(c, http.StatusBadRequest, "invalid_query", "Invalid query parameters")
		return
	}

	c.Next()
}
