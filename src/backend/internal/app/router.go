package app

import (
	"fmt"
	commonapp "honey-forge/internal/app"
	"honey-forge/internal/catalog"
	"honey-forge/internal/contract"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"honey-forge/src/backend/internal/platform/httpx"
	authservice "honey-forge/src/backend/modules/auth/service"
	profileservice "honey-forge/src/backend/modules/profiles/service"
)

var ginModeOnce sync.Once

func NewRouter(
	service *authservice.Service,
	logger *slog.Logger,
	allowedOrigin string,
	profileService *profileservice.Service,
	catalogService *catalog.Service,
) (*gin.Engine, error) {
	if err := validateOrigin(allowedOrigin); err != nil {
		return nil, err
	}

	ginModeOnce.Do(func() {
		gin.SetMode(gin.ReleaseMode)
	})

	router := commonapp.NewRouter()
	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false

	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("configuring trusted proxies: %w", err)
	}

	router.Use(recovery(logger))

	router.NoRoute(func(c *gin.Context) {
		httpx.WriteError(c, http.StatusNotFound, "resource_not_found", "Resource not found")
	})

	router.NoMethod(func(c *gin.Context) {
		httpx.WriteError(c, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	})

	browser, err := contract.NewBrowserPolicy([]string{allowedOrigin})
	if err != nil {
		return nil, err
	}
	if err := commonapp.RegisterServices(router, browser, service, profileService, catalogService, logger); err != nil {
		return nil, err
	}

	return router, nil
}
