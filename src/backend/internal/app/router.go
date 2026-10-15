package app

import (
	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

// CORS должен обработать preflight до общей проверки контракта.
func NewRouter(pre ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	if len(pre) > 0 {
		r.Use(pre...)
	}

	r.Use(contract.Middleware())
	r.NoRoute(func(c *gin.Context) {
		contract.Fail(c, contract.NewError("resource_not_found"))
	})
	r.NoMethod(func(c *gin.Context) {
		contract.Fail(c, contract.NewError("method_not_allowed"))
	})

	return r
}
