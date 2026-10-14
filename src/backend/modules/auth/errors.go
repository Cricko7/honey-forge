package auth

import "errors"

var (
	ErrAdminConfirmationRequired = errors.New("admin password confirmation required")
	ErrPermissionChanged         = errors.New("member permission changed")
	ErrPermissionProtected       = errors.New("administrator permissions are protected")
	ErrEmailTaken                = errors.New("email already registered")
	ErrInvalidCredentials        = errors.New("invalid credentials")
	ErrUnauthorized              = errors.New("unauthenticated")
	ErrAlreadyAuthenticated      = errors.New("already authenticated")
	ErrInvalidJoinCode           = errors.New("invalid organization join code")
	ErrForbidden                 = errors.New("insufficient permissions")
	ErrCSRF                      = errors.New("CSRF validation failed")
	ErrJoinCodeChanged           = errors.New("organization join code changed")
	ErrRevisionExhausted         = errors.New("revision exhausted")
	ErrNotFound                  = errors.New("record not found")
	ErrUnavailable               = errors.New("database unavailable")
)
