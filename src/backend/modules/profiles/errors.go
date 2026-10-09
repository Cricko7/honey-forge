package profiles

import (
	"errors"
	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
)

var (
	ErrUnauthorized         = errors.New("authentication required")
	ErrForbidden            = errors.New("insufficient permissions")
	ErrNotFound             = errors.New("resource not found")
	ErrValidation           = errors.New("request validation failed")
	ErrUnknownType          = errors.New("unknown trap type version")
	ErrTypeUnavailable      = errors.New("trap type is unavailable for new profiles")
	ErrConfigInvalid        = errors.New("trap configuration is invalid")
	ErrConfigTooLarge       = errors.New("trap configuration is too large")
	ErrIdempotencyConflict  = errors.New("request identifier was used with different content")
	ErrRequestUsed          = errors.New("request identifier belongs to a deleted resource")
	ErrRevisionExhausted    = errors.New("resource revision limit reached")
	ErrRevisionMismatch     = errors.New("resource has changed")
	ErrPreconditionRequired = errors.New("a precondition header is required")
	ErrInvalidPrecondition  = errors.New("invalid precondition header")
	ErrInUse                = errors.New("profile is assigned to a trap")
	ErrProfileChanged       = errors.New("profile revision has changed")
	ErrInvalidCursor        = errors.New("invalid cursor")
	ErrUnavailable          = errors.New("service is temporarily unavailable")
	ErrDatabaseUnavailable  = errors.New("database is temporarily unavailable")
)

type ValidationError struct {
	Cause  error
	Fields []httpx.FieldError
}

func (e *ValidationError) Error() string { return e.Cause.Error() }

func (e *ValidationError) Unwrap() error { return e.Cause }
