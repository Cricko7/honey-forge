package contract

import (
	"context"

	"github.com/gin-gonic/gin"
)

type Role string

const (
	Admin  Role = "admin"
	Viewer Role = "viewer"
	Agent  Role = "agent"
)

type Principal struct {
	UserID         ID
	OrganizationID ID
	TrapID         ID
	Role           Role
}

type principalKey struct{}

// WithPrincipal is for the authentication module, never for client supplied IDs.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

func SetPrincipal(c *gin.Context, p Principal) {
	c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), p))
}

func Authorize(ctx context.Context, roles ...Role) error {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return NewError("unauthenticated")
	}
	for _, role := range roles {
		if p.Role == role {
			return nil
		}
	}
	return NewError("forbidden")
}

func RequireRole(c *gin.Context, roles ...Role) bool {
	if err := Authorize(c.Request.Context(), roles...); err != nil {
		Fail(c, err.(*Error))
		return false
	}
	return true
}

func RequireOrganization(ctx context.Context, organizationID ID) error {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return NewError("unauthenticated")
	}
	if p.OrganizationID != organizationID {
		return NewError("resource_not_found")
	}
	return nil
}

func RequireAgentTrap(ctx context.Context, trapID ID) error {
	if err := AuthorizeCapability(ctx, AgentStream); err != nil {
		return err
	}
	principal, _ := PrincipalFrom(ctx)
	if !ValidID(string(principal.OrganizationID)) || !ValidID(string(principal.TrapID)) {
		return NewError("agent_unauthenticated")
	}
	if principal.TrapID != trapID {
		return NewError("resource_not_found")
	}
	return nil
}
