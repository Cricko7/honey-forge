package http

import (
	"reflect"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/platform/httpx"
	authcore "honey-forge/modules/auth"
)

func bindRequest(c *gin.Context, dst any) bool {
	return httpx.BindJSON(c, dst, func(raw []byte) bool {
		return authcore.StrictObject(raw, reflect.TypeOf(dst).Elem())
	})
}

func bodyLimit(c *gin.Context) {
	httpx.BodyLimit(c)
}

func emptyBody(c *gin.Context) bool {
	return httpx.EmptyBody(c)
}
