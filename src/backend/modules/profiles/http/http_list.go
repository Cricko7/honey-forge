package http

import (
	authhttp "honey-forge/src/backend/modules/auth/http"
	profilecore "honey-forge/src/backend/modules/profiles"
	"net/http"
	"net/url"
	"strconv"

	"honey-forge/src/backend/internal/platform/httpx"

	"github.com/gin-gonic/gin"
)

func (h *Handler) list(c *gin.Context) {
	if !httpx.EmptyBody(c) {
		return
	}

	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		httpx.WriteError(c, 400, "invalid_query", "Invalid query parameters")
		return
	}

	opts := profilecore.ListOptions{Limit: 50}
	for key, values := range query {
		if len(values) != 1 {
			httpx.WriteError(c, 400, "invalid_query", "Invalid query parameters")
			return
		}

		value := values[0]
		switch key {
		case "limit":
			opts.Limit, err = strconv.Atoi(value)
			if err != nil || opts.Limit < 1 || opts.Limit > 100 || strconv.Itoa(opts.Limit) != value {
				httpx.WriteError(c, 400, "invalid_query", "Invalid query parameters")
				return
			}
		case "type_id":
			if !profilecore.ValidTypeID(value) {
				httpx.WriteError(c, 400, "invalid_query", "Invalid query parameters")
				return
			}

			opts.TypeID = value
		case "cursor":
			if value == "" || len(value) > 2048 {
				httpx.WriteError(c, 400, "invalid_cursor", "Invalid pagination cursor")
				return
			}

			opts.Cursor = value
		default:
			httpx.WriteError(c, 400, "invalid_query", "Invalid query parameters")
			return
		}
	}

	a, _ := authhttp.Context(c)
	page, err := h.service.List(c.Request.Context(), a, opts)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, page)
}
