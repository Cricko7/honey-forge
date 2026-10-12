package app

import (
	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
)

func RegisterCatalog(router *gin.Engine, browser *contract.BrowserPolicy, authenticate Authenticate, service *catalog.Service) {
	handler := catalog.NewHandler(service)
	RegisterOperator(router, browser, "GET", "/api/trap-types", contract.ReadResources, authenticate, handler.List)
	RegisterOperator(router, browser, "GET", "/api/trap-types/:type_id/versions/:type_version", contract.ReadResources, authenticate, handler.Read)
}
