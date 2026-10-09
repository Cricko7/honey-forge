package app

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"honey-forge/internal/catalog"
	"honey-forge/internal/contract"
	authhttp "honey-forge/src/backend/modules/auth/http"
	authservice "honey-forge/src/backend/modules/auth/service"
	profilehttp "honey-forge/src/backend/modules/profiles/http"
	profileservice "honey-forge/src/backend/modules/profiles/service"
	"log/slog"
	"net/http"
	"time"
)

// RegisterServices connects both feature modules and the catalog to one real session.
func RegisterServices(router *gin.Engine, browser *contract.BrowserPolicy, auth *authservice.Service, profiles *profileservice.Service, catalogService *catalog.Service, logger *slog.Logger) error {
	router.Use(func(c *gin.Context) {
		c.Set("request_id", c.Writer.Header().Get("X-Request-ID"))
		c.Header("X-Content-Type-Options", "nosniff")
		started := time.Now()
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		defer func() {
			logger.InfoContext(ctx, "HTTP request completed", "request_id", c.Writer.Header().Get("X-Request-ID"), "method", c.Request.Method, "route", c.FullPath(), "status", c.Writer.Status(), "duration_us", time.Since(started).Microseconds())
		}()
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !browser.CheckOrigin(c) {
				return
			}
		}
		c.Next()
	})
	handler := authhttp.NewHandler(auth, logger)
	if err := handler.RegisterRoutes(router); err != nil {
		return fmt.Errorf("register auth: %w", err)
	}
	if err := profilehttp.NewHandler(profiles, logger).RegisterRoutes(router, handler.RequireSession()); err != nil {
		return fmt.Errorf("register profiles: %w", err)
	}
	RegisterSessionCatalog(router, handler, catalogService)
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	return nil
}

func RegisterSessionCatalog(router *gin.Engine, auth *authhttp.Handler, service *catalog.Service) {
	handler := catalog.NewHandler(service)
	principal := func(c *gin.Context) {
		session, ok := authhttp.Context(c)
		if !ok {
			contract.Fail(c, contract.NewError("unauthenticated"))
			return
		}
		contract.SetPrincipal(c, contract.Principal{UserID: contract.ID(session.UserID), OrganizationID: contract.ID(session.OrganizationID), Role: contract.Role(session.Role)})
		c.Next()
	}
	router.GET("/api/trap-types", auth.RequireSession(), principal, contract.RESTBody(), handler.List)
	router.GET("/api/trap-types/:type_id/versions/:type_version", auth.RequireSession(), principal, contract.RESTBody(), handler.Read)
}
