package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
)

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			panic(err)
		}

		random[6] = random[6]&0x0f | 0x40
		random[8] = random[8]&0x3f | 0x80
		id := fmt.Sprintf("%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")

		start := time.Now()
		c.Next()

		level := slog.LevelInfo
		if c.Writer.Status() >= 500 {
			level = slog.LevelError
		}

		logger.Log(c.Request.Context(), level, "HTTP request completed",
			"request_id", id,
			"method", c.Request.Method,
			"route", c.FullPath(),
			"status", c.Writer.Status(),
			"response_bytes", c.Writer.Size(),
			"duration_us", time.Since(start).Microseconds(),
			"client_ip", c.ClientIP(),
			"error_code", c.GetString("error_code"),
		)
	}
}

func recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if value := recover(); value != nil {
				// Never dump request headers or panic values: they may contain tokens.
				logger.ErrorContext(c.Request.Context(), "HTTP panic recovered",
					"request_id", c.GetString("request_id"),
					"panic_type", fmt.Sprintf("%T", value),
					"stack", string(debug.Stack()),
				)

				if !c.Writer.Written() {
					httpx.WriteError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
				}
				c.Abort()
			}
		}()

		c.Next()
	}
}

func requestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func originProtection(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if len(c.Request.Header.Values("Origin")) != 1 || c.GetHeader("Origin") != allowedOrigin {
				httpx.WriteError(c, http.StatusForbidden, "origin_not_allowed", "Origin is not allowed")
				return
			}
		}

		c.Next()
	}
}
