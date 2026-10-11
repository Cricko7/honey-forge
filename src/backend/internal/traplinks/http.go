package traplinks

import (
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	authhttp "honey-forge/modules/auth/http"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler { return &Handler{service, logger} }

func (h *Handler) Register(router *gin.Engine, session gin.HandlerFunc) {
	group := router.Group("/api/trap-links", session, func(c *gin.Context) {
		if a, ok := authhttp.Context(c); ok {
			contract.SetPrincipal(c, contract.Principal{UserID: contract.ID(a.UserID), OrganizationID: contract.ID(a.OrganizationID), Role: contract.Role(a.Role)})
		}
		if _, err := organization(c.Request.Context(), c.Request.Method != "GET"); err != nil {
			h.fail(c, err)
			return
		}
		c.Next()
	}, contract.RESTBody())
	group.GET("", h.list)
	group.POST("", h.create)
	group.DELETE("/:source/:target", h.delete)
}

func (h *Handler) list(c *gin.Context) {
	if _, ok := contract.RequestQuery(c); !ok || !httpx.EmptyBody(c) {
		return
	}
	links, err := h.service.List(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"items": links})
}

func (h *Handler) create(c *gin.Context) {
	if _, ok := contract.RequestQuery(c); !ok {
		return
	}
	var link Link
	if !httpx.BindJSON(c, &link, nil) {
		return
	}
	result, err := h.service.Create(c.Request.Context(), link)
	if err != nil {
		h.fail(c, err)
		return
	}
	contract.Created(c, "/api/trap-links/"+result.SourceID+"/"+result.TargetID, result)
}

func (h *Handler) delete(c *gin.Context) {
	source, ok := contract.RequireID(c, "source")
	if !ok {
		return
	}
	target, ok := contract.RequireID(c, "target")
	if !ok {
		return
	}
	if _, ok := contract.RequestQuery(c); !ok || !httpx.EmptyBody(c) {
		return
	}
	if err := h.service.Delete(c.Request.Context(), string(source), string(target)); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(204)
}

func (h *Handler) fail(c *gin.Context, err error) {
	var api *contract.Error
	if errors.As(err, &api) {
		contract.Fail(c, api)
		return
	}
	h.logger.ErrorContext(c.Request.Context(), "trap link request failed", "error", err)
	contract.Fail(c, contract.NewError("internal_error"))
}
