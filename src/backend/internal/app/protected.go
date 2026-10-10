package app

import (
	"errors"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

// Authenticate is the consumed boundary supplied by real module 02 session validation.
// The returned CSRF secret comes from the validated server session.
type Authenticate func(*gin.Context) (contract.Principal, string, error)

func RegisterOperator(router *gin.Engine, policy *contract.BrowserPolicy, method, path string, capability contract.Capability, authenticate Authenticate, handler gin.HandlerFunc, ownership ...gin.HandlerFunc) {
	handlers := []gin.HandlerFunc{func(c *gin.Context) {
		if authenticate == nil {
			contract.Fail(c, contract.NewError("unauthenticated"))
			return
		}
		principal, csrf, err := authenticate(c)
		if err != nil {
			var apiError *contract.Error
			if errors.As(err, &apiError) {
				contract.Fail(c, apiError)
			} else {
				contract.Fail(c, contract.NewError("internal_error"))
			}
			return
		}
		contract.SetPrincipal(c, principal)
		if !policy.Guard(c, csrf, capability) {
			return
		}
		c.Next()
	}}
	handlers = append(handlers, ownership...)
	handlers = append(handlers, contract.RESTBody(), handler)
	router.Handle(method, path, handlers...)
}
