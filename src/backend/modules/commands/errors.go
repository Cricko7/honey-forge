package commands

import "errors"

var (
	ErrUnauthorized            = errors.New("unauthenticated")
	ErrForbidden               = errors.New("forbidden")
	ErrNotFound                = errors.New("resource_not_found")
	ErrValidation              = errors.New("validation_failed")
	ErrInvalidParams           = errors.New("command_params_invalid")
	ErrUnsupportedAction       = errors.New("unsupported_action")
	ErrProfileChanged          = errors.New("profile_changed")
	ErrConfigurationNotApplied = errors.New("configuration_not_applied")
	ErrInProgress              = errors.New("command_in_progress")
	ErrIdempotencyConflict     = errors.New("idempotency_conflict")
	ErrUnavailable             = errors.New("database_unavailable")
	ErrExpired                 = errors.New("command_expired")
	ErrStaleLease              = errors.New("stale_command_lease")
	ErrResultInvalid           = errors.New("command_result_invalid")
	ErrResultConflict          = errors.New("command_result_conflict")
)
