package postgres

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUnavailable distinguishes retryable infrastructure failures from domain or
// constraint errors without copying a driver's potentially sensitive message.
func IsUnavailable(err error) bool {
	if IsConnectionError(err) {
		return true
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return strings.HasPrefix(pgError.Code, "08") || strings.HasPrefix(pgError.Code, "57P0") || pgError.Code == "53300" || pgError.Code == "57014"
	}
	return false
}

// IsConnectionError classifies transport and context failures. Feature-specific
// SQLSTATE policies can use it without changing their published error mapping.
func IsConnectionError(err error) bool {
	var connection *pgconn.ConnectError
	var network net.Error
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &connection) || errors.As(err, &network) || pgconn.SafeToRetry(err)
}

// Keep the inspectable cause while making default boundary logging safe even
// when a driver parsing error embeds a connection string containing credentials.
type connectionError struct {
	message string
	cause   error
}

func (e *connectionError) Error() string { return e.message }
func (e *connectionError) Unwrap() error { return e.cause }
