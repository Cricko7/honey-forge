package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/stream"
	authhttp "honey-forge/modules/auth/http"
	authservice "honey-forge/modules/auth/service"
	"honey-forge/modules/catalog"
	profilehttp "honey-forge/modules/profiles/http"
	profileservice "honey-forge/modules/profiles/service"
)

// RegisterServices connects modules 02, 03 and 04 in plan order to one real session.
func RegisterServices(
	router *gin.Engine,
	browser *contract.BrowserPolicy,
	auth *authservice.Service,
	profiles *profileservice.Service,
	catalogService *catalog.Service,
	logger *slog.Logger,
) error {
	router.Use(operatorMiddleware(browser, logger))

	handler := authhttp.NewHandler(auth, logger)
	if err := handler.RegisterRoutes(router); err != nil {
		return fmt.Errorf("register auth: %w", err)
	}

	RegisterSessionCatalog(router, handler, catalogService)

	if err := profilehttp.NewHandler(profiles, logger).RegisterRoutes(router, handler.RequireSession()); err != nil {
		return fmt.Errorf("register profiles: %w", err)
	}

	router.GET("/healthz", healthCheck)
	return nil
}

func operatorMiddleware(browser *contract.BrowserPolicy, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("request_id", c.Writer.Header().Get("X-Request-ID"))
		c.Header("X-Content-Type-Options", "nosniff")
		if c.Request.URL.Path == stream.AgentPath {
			c.Next()
			return
		}

		started := time.Now()
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		defer func() {
			logger.InfoContext(ctx, "HTTP request completed",
				"request_id", c.Writer.Header().Get("X-Request-ID"),
				"method", c.Request.Method,
				"route", c.FullPath(),
				"status", c.Writer.Status(),
				"duration_us", time.Since(started).Microseconds(),
			)
		}()

		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !browser.CheckOrigin(c) {
				return
			}
		}

		c.Next()
	}
}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func RegisterSessionCatalog(router *gin.Engine, auth *authhttp.Handler, catalogService *catalog.Service) {
	handler := catalog.NewHandler(catalogService)
	principal := func(c *gin.Context) {
		session, ok := authhttp.Context(c)
		if !ok {
			contract.Fail(c, contract.NewError("unauthenticated"))
			return
		}
		contract.SetPrincipal(c, contract.Principal{
			UserID:         contract.ID(session.UserID),
			OrganizationID: contract.ID(session.OrganizationID),
			Role:           contract.Role(session.Role),
		})
		c.Next()
	}

	router.GET("/api/trap-types", auth.RequireSession(), principal, contract.RESTBody(), handler.List)
	router.GET(
		"/api/trap-types/:type_id/versions/:type_version",
		auth.RequireSession(),
		principal,
		contract.RESTBody(),
		handler.Read,
	)
}
