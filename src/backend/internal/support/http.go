package support

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	authhttp "honey-forge/modules/auth/http"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) Register(router *gin.Engine, session gin.HandlerFunc) {
	router.POST("/api/support/messages", session, httpx.BodyLimit, h.send)
}

func (h *Handler) send(c *gin.Context) {
	if _, ok := contract.RequestQuery(c); !ok {
		return
	}

	var message Message
	if !httpx.BindJSON(c, &message, nil) {
		return
	}

	actor, _ := authhttp.Context(c)
	err := h.service.Send(c.Request.Context(), actor, message)

	switch {
	case err == nil:
		c.Status(http.StatusNoContent)
	case errors.Is(err, ErrUnauthenticated):
		httpx.WriteError(c, http.StatusUnauthorized, "unauthenticated", "Войдите в аккаунт")
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(c, http.StatusUnprocessableEntity, "validation_failed", "Проверьте тему и описание обращения")
	default:
		// Provider errors can include addresses and credentials; never log their text.
		h.logger.ErrorContext(c.Request.Context(), "support delivery failed", "error_type", fmt.Sprintf("%T", err))
		httpx.WriteError(c, http.StatusServiceUnavailable, "support_unavailable", "Не удалось отправить письмо. Свяжитесь с поддержкой в Telegram")
	}
}
