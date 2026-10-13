package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/platform/httpx"
	authcore "honey-forge/modules/auth"
)

// RequireSession passes a verified identity to other operator feature handlers.
func (h *Handler) RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(c.Request.Header.Values("Authorization")) > 0 {
			raw, err := authcore.BearerToken(c.Request.Header)
			if err != nil {
				h.fail(c, "API key authentication failed", err)
				return
			}

			sess, err := h.service.AuthenticateAPIKey(c.Request.Context(), raw)
			if err != nil {
				h.fail(c, "API key authentication failed", err)
				return
			}

			c.Set("auth_context", sess.AuthContext())
			c.Next()

			return
		}

		sess, err := h.service.ResolveSession(c.Request.Context(), readSessionCookie(c))
		if err != nil {
			h.fail(c, "session authentication failed", err)
			return
		}

		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			if len(c.Request.Header.Values("X-CSRF-Token")) != 1 {
				h.fail(c, "CSRF rejected", authcore.ErrCSRF)
				return
			}

			if err := authcore.CheckCSRF(sess, c.GetHeader("X-CSRF-Token")); err != nil {
				h.fail(c, "CSRF rejected", err)
				return
			}
		}

		c.Set("resolved_session", sess)
		c.Set("auth_context", sess.AuthContext())
		c.Next()
	}
}

// Credential management stays protected by the browser session and CSRF.
func (h *Handler) requireBrowserSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(c.Request.Header.Values("Authorization")) > 0 {
			h.fail(c, "browser session required", authcore.ErrForbidden)
			return
		}

		h.RequireSession()(c)
	}
}

func Context(c *gin.Context) (authcore.AuthContext, bool) {
	value, ok := c.Get("auth_context")
	if !ok {
		return authcore.AuthContext{}, false
	}

	actor, ok := value.(authcore.AuthContext)

	return actor, ok
}

func resolvedSession(c *gin.Context) authcore.ResolvedSession {
	return c.MustGet("resolved_session").(authcore.ResolvedSession)
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
