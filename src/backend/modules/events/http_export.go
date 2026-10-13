package events

import (
	"fmt"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
)

func (h *Handler) export(c *gin.Context) {
	if !httpx.EmptyBody(c) {
		return
	}

	values, ok := contract.RequestQuery(c, "format", "trap_id", "type_id", "event_type", "from", "to", "source_ip", "session_id")
	if !ok {
		return
	}

	format := values.Get("format")
	if format != "csv" && format != "stix" {
		contract.Fail(c, contract.NewError("invalid_query"))
		return
	}

	q, queryErr := parseEventFilters(values)
	if queryErr != nil {
		h.fail(c, queryErr)
		return
	}

	file, err := os.CreateTemp("", "honeyforge-events-*")
	if err != nil {
		h.fail(c, err)
		return
	}

	defer func() {
		if err := file.Close(); err != nil {
			h.logger.ErrorContext(c.Request.Context(), "close event export", "error", err)
		}

		if err := os.Remove(file.Name()); err != nil {
			h.logger.ErrorContext(c.Request.Context(), "remove event export", "error", err)
		}
	}()

	if err := h.service.Export(c.Request.Context(), q, format, file); err != nil {
		h.fail(c, err)
		return
	}

	size, err := file.Seek(0, 2)
	if err != nil {
		h.fail(c, err)
		return
	}

	if _, err := file.Seek(0, 0); err != nil {
		h.fail(c, err)
		return
	}

	extension, contentType := "csv", "text/csv; charset=utf-8"
	if format == "stix" {
		extension, contentType = "stix.json", "application/stix+json;version=2.1"
	}

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="events-%s.%s"`, time.Now().UTC().Format("20060102-150405"), extension))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(200, size, contentType, file, nil)
}
