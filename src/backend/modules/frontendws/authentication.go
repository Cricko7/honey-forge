package frontendws

import (
	"context"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/postgres"
	"honey-forge/modules/auth"
)

func (h *Handler) authenticate(ctx context.Context, cookie string, bearer bool) (contract.Principal, error) {
	var sess auth.ResolvedSession
	var err error
	if bearer {
		keys, ok := h.sessions.(apiKeys)
		if !ok {
			return contract.Principal{}, contract.NewError("unauthenticated")
		}

		sess, err = keys.ResolveAPIKey(ctx, cookie)
	} else {
		sess, err = h.sessions.ResolveSession(ctx, cookie)
	}
	if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrNotFound) {
		return contract.Principal{}, contract.NewError("unauthenticated")
	}

	if err != nil {
		return contract.Principal{}, err
	}

	a := sess.AuthContext()
	p := contract.Principal{UserID: contract.ID(a.UserID), OrganizationID: contract.ID(a.OrganizationID), Role: contract.Role(a.Role)}
	if err := contract.AuthorizeCapability(contract.WithPrincipal(ctx, p), contract.FrontendStream); err != nil {
		return p, err
	}

	return p, nil
}

func (h *Handler) httpFailure(c *gin.Context, err error) {
	var rateError *auth.APIKeyRateError
	if errors.As(err, &rateError) {
		c.Header("Retry-After", strconv.Itoa(rateError.RetryAfter))
		contract.Fail(c, contract.NewError("rate_limited"))
		return
	}

	if errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrNotFound) {
		contract.Fail(c, contract.NewError("unauthenticated"))
		return
	}

	var api *contract.Error
	if !errors.As(err, &api) {
		api = contract.NewError("internal_error")
		if postgres.IsUnavailable(err) || errors.Is(err, auth.ErrUnavailable) {
			api = contract.NewError("database_unavailable")
		}

		h.log(err)
	}

	contract.Fail(c, api)
}
