package app

import (
	"honey-forge/internal/contract"

	"github.com/gin-gonic/gin"
)

func NewRouter() *gin.Engine {
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err)
	}
	r.Use(contract.Middleware())
	r.NoRoute(func(c *gin.Context) { contract.Fail(c, contract.NewError("resource_not_found")) })
	r.NoMethod(func(c *gin.Context) { contract.Fail(c, contract.NewError("method_not_allowed")) })
	return r
}
