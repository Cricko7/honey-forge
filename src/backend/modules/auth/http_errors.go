package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
)

func (h *Handler) fail(c *gin.Context, event string, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "Internal server error"

	switch {
	case errors.Is(err, ErrEmailTaken):
		status, code, message = 409, "email_in_use", "Email is already registered"
	case errors.Is(err, ErrAlreadyAuthenticated):
		status, code, message = 409, "already_authenticated", "Already authenticated"
	case errors.Is(err, ErrInvalidCredentials):
		status, code, message = 401, "invalid_credentials", "Invalid email or password"
	case errors.Is(err, ErrUnauthorized):
		status, code, message = 401, "unauthenticated", "Authentication required"
	case errors.Is(err, ErrForbidden):
		status, code, message = 403, "forbidden", "Insufficient permissions"
	case errors.Is(err, ErrCSRF):
		status, code, message = 403, "csrf_failed", "CSRF validation failed"
	case errors.Is(err, ErrInvalidJoinCode):
		status, code, message = 422, "invalid_join_code", "Invalid organization join code"
	case errors.Is(err, ErrJoinCodeChanged):
		status, code, message = 409, "join_code_changed", "Organization join code has changed"
	case errors.Is(err, ErrRevisionExhausted):
		status, code, message = 409, "revision_exhausted", "Resource revision limit reached"
	case errors.Is(err, ErrNotFound):
		status, code, message = 404, "resource_not_found", "Resource not found"
	case errors.Is(err, ErrUnavailable):
		status, code, message = 503, "database_unavailable", "Database is temporarily unavailable"
		c.Header("Retry-After", "5")
	}

	level := slog.LevelWarn
	if status >= 500 {
		level = slog.LevelError
	}

	// PostgreSQL Detail can contain email, session hashes or join codes.
	var pgErr *pgconn.PgError
	sqlState := ""
	if errors.As(err, &pgErr) {
		sqlState = pgErr.Code
	}

	h.logger.Log(c.Request.Context(), level, event,
		"request_id", c.GetString("request_id"), "error_code", code,
		"error_type", fmt.Sprintf("%T", err), "sql_state", sqlState,
	)
	httpx.WriteError(c, status, code, message)
}
