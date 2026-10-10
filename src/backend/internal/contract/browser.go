package contract

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
)

type Capability string

const (
	ReadResources     Capability = "read_resources"
	WriteResources    Capability = "write_resources"
	ManageCredentials Capability = "manage_credentials"
	FrontendStream    Capability = "frontend_stream"
	AgentStream       Capability = "agent_stream"
)

func AuthorizeCapability(ctx context.Context, capability Capability) error {
	switch capability {
	case ReadResources, FrontendStream:
		return Authorize(ctx, Admin, Viewer)
	case WriteResources, ManageCredentials:
		return Authorize(ctx, Admin)
	case AgentStream:
		return Authorize(ctx, Agent)
	default:
		return NewError("forbidden")
	}
}

type BrowserPolicy struct{ origins map[string]bool }

func NewBrowserPolicy(origins []string) (*BrowserPolicy, error) {
	p := &BrowserPolicy{origins: map[string]bool{}}
	for _, origin := range origins {
		if !ValidHTTPSOrigin(origin) {
			return nil, fmt.Errorf("invalid browser origin")
		}
		p.origins[origin] = true
	}
	return p, nil
}

func ValidHTTPSOrigin(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery
}

// CORSMiddleware emits CORS headers for the allowlisted browser origins and
// answers preflight (OPTIONS) requests. A separate frontend origin
// (app.example.org → api.example.org) otherwise fails: the Origin check alone
// rejects nothing, but the browser blocks the response without these headers.
//
// Credentials (the __Host-session cookie) require echoing the exact Origin and
// Vary: Origin; a wildcard is not permitted with Allow-Credentials. Only
// allowlisted origins receive headers, so this does not widen access beyond
// what CheckOrigin already enforces for mutations.
func (p *BrowserPolicy) CORSMiddleware() gin.HandlerFunc {
	const allowHeaders = "Content-Type, X-CSRF-Token, X-Expected-Revision, If-Match, If-None-Match"
	const allowMethods = "GET, POST, PATCH, DELETE, OPTIONS"
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowed := origin != "" && p.origins[origin]
		if allowed {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions {
			// Preflight: respond here and never reach handlers. Disallowed
			// origins get a bare 204 without CORS headers (browser blocks).
			if allowed {
				h := c.Writer.Header()
				h.Set("Access-Control-Allow-Methods", allowMethods)
				if requested := c.GetHeader("Access-Control-Request-Headers"); requested != "" {
					h.Set("Access-Control-Allow-Headers", requested)
				} else {
					h.Set("Access-Control-Allow-Headers", allowHeaders)
				}
				h.Set("Access-Control-Max-Age", "600")
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (p *BrowserPolicy) CheckOrigin(c *gin.Context) bool {
	values := c.Request.Header.Values("Origin")
	if len(values) != 1 || !p.origins[values[0]] {
		Fail(c, NewError("origin_not_allowed"))
		return false
	}
	return true
}

// Guard is called after real session authentication. The expected CSRF value comes
// from that session, never from a client cookie/header pair alone.
func (p *BrowserPolicy) Guard(c *gin.Context, expectedCSRF string, capability Capability) bool {
	if _, ok := PrincipalFrom(c.Request.Context()); !ok {
		Fail(c, NewError("unauthenticated"))
		return false
	}
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Request.Method != "OPTIONS" {
		if !p.CheckOrigin(c) {
			return false
		}
		values := c.Request.Header.Values("X-CSRF-Token")
		if len(values) != 1 || expectedCSRF == "" || subtle.ConstantTimeCompare([]byte(values[0]), []byte(expectedCSRF)) != 1 {
			Fail(c, NewError("csrf_failed"))
			return false
		}
	}
	if err := AuthorizeCapability(c.Request.Context(), capability); err != nil {
		Fail(c, err.(*Error))
		return false
	}
	return true
}
