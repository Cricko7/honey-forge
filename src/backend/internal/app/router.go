package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
	authhttp "github.com/Cricko7/honey-forge/src/backend/modules/auth/http"
	authservice "github.com/Cricko7/honey-forge/src/backend/modules/auth/service"
	profilehttp "github.com/Cricko7/honey-forge/src/backend/modules/profiles/http"
	profileservice "github.com/Cricko7/honey-forge/src/backend/modules/profiles/service"
)

var ginModeOnce sync.Once

func NewRouter(service *authservice.Service, logger *slog.Logger, allowedOrigin string, profileService *profileservice.Service) (*gin.Engine, error) {
	if err := validateOrigin(allowedOrigin); err != nil {
		return nil, err
	}

	ginModeOnce.Do(func() {
		gin.SetMode(gin.ReleaseMode)
	})

	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false

	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("configuring trusted proxies: %w", err)
	}

	router.Use(requestLogger(logger), recovery(logger), requestContext(), originProtection(allowedOrigin))

	router.NoRoute(func(c *gin.Context) {
		httpx.WriteError(c, http.StatusNotFound, "resource_not_found", "Resource not found")
	})

	router.NoMethod(func(c *gin.Context) {
		httpx.WriteError(c, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	})

	handler := authhttp.NewHandler(service, logger)
	if err := handler.RegisterRoutes(router); err != nil {
		return nil, fmt.Errorf("registering auth routes: %w", err)
	}
	if err := profilehttp.NewHandler(profileService, logger).RegisterRoutes(router, handler.RequireSession()); err != nil {
		return nil, fmt.Errorf("registering profile routes: %w", err)
	}

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return router, nil
}
