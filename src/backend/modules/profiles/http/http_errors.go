package http

import (
	"errors"
	"fmt"
	profilecore "honey-forge/modules/profiles"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"honey-forge/internal/platform/httpx"
)

func (h *Handler) fail(c *gin.Context, err error) {
	status, code, message := 500, "internal_error", "Internal server error"
	for _, entry := range []struct {
		err           error
		status        int
		code, message string
	}{
		{profilecore.ErrUnauthorized, 401, "unauthenticated", "Authentication required"},
		{profilecore.ErrForbidden, 403, "forbidden", "Insufficient permissions"},
		{profilecore.ErrNotFound, 404, "resource_not_found", "Resource not found"},
		{profilecore.ErrValidation, 422, "validation_failed", "Request validation failed"},
		{profilecore.ErrUnknownType, 422, "unknown_trap_type", "Unknown trap type version"},
		{profilecore.ErrTypeUnavailable, 409, "trap_type_unavailable", "Trap type is unavailable for new profiles"},
		{profilecore.ErrConfigInvalid, 422, "config_invalid", "Trap configuration is invalid"},
		{profilecore.ErrConfigTooLarge, 422, "config_too_large", "Trap configuration is too large"},
		{profilecore.ErrIdempotencyConflict, 409, "idempotency_conflict", "Request identifier was used with different content"},
		{profilecore.ErrRequestUsed, 409, "request_already_used", "Request identifier belongs to a deleted resource"},
		{profilecore.ErrRevisionExhausted, 409, "revision_exhausted", "Resource revision limit reached"},
		{profilecore.ErrRevisionMismatch, 412, "revision_mismatch", "Resource has changed"},
		{profilecore.ErrPreconditionRequired, 428, "precondition_required", "A precondition header is required"},
		{profilecore.ErrInvalidPrecondition, 400, "invalid_precondition", "Invalid precondition header"},
		{profilecore.ErrInUse, 409, "profile_in_use", "Profile is assigned to a trap"},
		{profilecore.ErrInvalidCursor, 400, "invalid_cursor", "Invalid pagination cursor"},
		{profilecore.ErrDatabaseUnavailable, 503, "database_unavailable", "Database is temporarily unavailable"},
		{profilecore.ErrUnavailable, 503, "service_unavailable", "Service is temporarily unavailable"},
	} {
		if errors.Is(err, entry.err) {
			status, code, message = entry.status, entry.code, entry.message
			break
		}
	}

	if status == 503 {
		c.Header("Retry-After", "5")
	}

	var fields []httpx.FieldError
	var validation *profilecore.ValidationError
	if errors.As(err, &validation) {
		fields = validation.Fields[:min(len(validation.Fields), 20)]
	}

	var pgErr *pgconn.PgError
	sqlState := ""
	if errors.As(err, &pgErr) {
		sqlState = pgErr.Code
	}

	level := slog.LevelWarn
	if status >= 500 {
		level = slog.LevelError
	}

	h.logger.Log(c.Request.Context(), level, "profile request failed", "request_id", c.GetString("request_id"), "error_code", code, "error_type", fmt.Sprintf("%T", err), "sql_state", sqlState)
	httpx.WriteError(c, status, code, message, fields...)
}
