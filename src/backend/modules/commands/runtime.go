package commands

import (
	"strings"
	"unicode"
)

var runtimeMessages = map[string]string{
	"port_unavailable":       "Port is unavailable",
	"config_apply_failed":    "Configuration could not be applied",
	"config_rollback_failed": "Configuration rollback failed",
	"runtime_start_failed":   "Runtime could not start",
	"runtime_stop_failed":    "Runtime could not stop",
	"unsupported_type":       "Trap type is unsupported by this agent",
	"unsupported_action":     "Action is unsupported by this agent",
	"command_expired":        "Command has expired",
	"buffer_unavailable":     "Telemetry buffer is unavailable",
}

// NormalizeRuntimeError rejects unknown codes and prevents an agent supplied
// diagnostic from exposing captured data through operator status messages.
func NormalizeRuntimeError(value *RuntimeError) error {
	message, ok := runtimeMessages[value.Code]
	if !ok || len(value.Message) > 200 || strings.IndexFunc(value.Message, unicode.IsControl) >= 0 {
		return ErrResultInvalid
	}

	value.Message = message
	return nil
}
