package stream

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const resourceScript = `"use strict";
document.documentElement.dataset.resources = "ready";
`

const resourcePage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Resource Library</title></head>
<body><main><h1>Resource Library</h1><p>Shared web resources.</p><p><a href="/assets/stream">Browser resource script</a></p></main><script src="/assets/stream" defer></script></body></html>
`

// Resource is the same public asset for ordinary requests and failed authentication.
// It never includes request values, credentials, or application metadata.
func Resource(c *gin.Context) {
	resource(c, "application/javascript; charset=utf-8", resourceScript)
}

func ResourceIndex(c *gin.Context) {
	c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	resource(c, "text/html; charset=utf-8", resourcePage)
}

func resource(c *gin.Context, contentType, body string) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Length", strconv.Itoa(len(body)))
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", contentType)
		c.Status(http.StatusOK)
	} else {
		c.Data(http.StatusOK, contentType, []byte(body))
	}

	c.Abort()
}
