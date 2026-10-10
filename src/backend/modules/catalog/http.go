package catalog

import (
	"errors"
	"honey-forge/internal/contract"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(c *gin.Context) {
	values, ok := contract.RequestQuery(c, "limit", "cursor", "type_id", "available_for_new_profiles")
	if !ok {
		return
	}

	list, queryError := contract.ParseListQuery(values, "type_id", "available_for_new_profiles")
	if queryError != nil {
		contract.Fail(c, queryError)
		return
	}

	query := Query{Limit: list.Limit, Cursor: list.Cursor, TypeID: values.Get("type_id")}
	if _, provided := values["type_id"]; provided && !contract.ValidTypeID(query.TypeID) {
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}

	if value, provided := values["available_for_new_profiles"]; provided {
		if value[0] != "true" && value[0] != "false" {
			contract.Fail(c, contract.NewError("invalid_query"))
			return
		}

		available := value[0] == "true"
		query.Available = &available
	}

	page, etag, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		fail(c, err)
		return
	}

	if query.Cursor == "" {
		contract.CatalogResponse(c, etag, page)
		return
	}

	// Conditional reads only apply to the first page. All pages share the snapshot tag.
	c.Header("Cache-Control", "private, max-age=0, must-revalidate")
	c.Header("ETag", etag)
	c.JSON(200, page)
}

func (h *Handler) Read(c *gin.Context) {
	if _, ok := contract.RequestQuery(c); !ok {
		return
	}

	id := c.Param("type_id")
	value := c.Param("type_version")
	if !contract.ValidTypeID(id) || value == "" {
		contract.Fail(c, contract.NewError("invalid_id"))
		return
	}

	for _, character := range value {
		if character < '0' || character > '9' {
			contract.Fail(c, contract.NewError("invalid_id"))
			return
		}
	}

	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version < 1 || version > contract.MaxRevision {
		contract.Fail(c, contract.NewError("invalid_id"))
		return
	}

	entry, etag, err := h.service.Read(c.Request.Context(), id, contract.TypeVersion(version))
	if err != nil {
		fail(c, err)
		return
	}

	contract.CatalogResponse(c, etag, entry)
}

func fail(c *gin.Context, err error) {
	var apiError *contract.Error
	if errors.As(err, &apiError) {
		contract.Fail(c, apiError)
		return
	}

	slog.ErrorContext(c.Request.Context(), "catalog request failed", "request_id", c.Writer.Header().Get("X-Request-ID"), "error", err)
	contract.Fail(c, contract.NewError("internal_error"))
}
