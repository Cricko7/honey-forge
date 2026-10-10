package contract

import "github.com/gin-gonic/gin"

type FieldError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Error struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
	Status  int          `json:"-"`
}

func (e *Error) Error() string { return e.Code }

type ErrorResponse struct {
	Error     *Error `json:"error"`
	RequestID ID     `json:"request_id"`
}

var errorsByCode = map[string]struct {
	status  int
	message string
}{
	"config_invalid": {422, "Trap configuration is invalid"}, "telemetry_invalid": {422, "Telemetry event is invalid"}, "unsupported_action": {422, "Action is not supported by this type version"}, "command_params_invalid": {422, "Command parameters are invalid"}, "command_result_invalid": {422, "Command result is invalid"},
	"invalid_json": {400, "Invalid JSON body"}, "invalid_query": {400, "Invalid query parameters"}, "invalid_id": {400, "Invalid resource identifier"}, "invalid_cursor": {400, "Invalid pagination cursor"}, "invalid_precondition": {400, "Invalid precondition header"},
	"unauthenticated": {401, "Authentication required"}, "invalid_credentials": {401, "Invalid email or password"}, "agent_unauthenticated": {401, "Invalid agent credentials"},
	"forbidden": {403, "Insufficient permissions"}, "csrf_failed": {403, "CSRF validation failed"}, "origin_not_allowed": {403, "Origin is not allowed"},
	"resource_not_found": {404, "Resource not found"}, "method_not_allowed": {405, "Method not allowed"},
	"idempotency_conflict": {409, "Request identifier was used with different content"}, "request_already_used": {409, "Request identifier belongs to a deleted resource"}, "revision_exhausted": {409, "Resource revision limit reached"},
	"revision_mismatch": {412, "Resource has changed"}, "body_too_large": {413, "Request body is too large"}, "unsupported_media_type": {415, "Expected application/json"},
	"validation_failed": {422, "Request validation failed"}, "invalid_time_range": {422, "Invalid time range"}, "config_too_large": {422, "Trap configuration is too large"},
	"invalid_ws_protocol": {400, "Invalid WebSocket subprotocol"}, "invalid_message": {400, "Invalid WebSocket message"},
	"unsupported_type": {422, "Trap type version is not supported"}, "unknown_configuration": {422, "Unknown applied configuration"},
	"stale_command_lease": {409, "Command lease is no longer valid"}, "command_expired": {409, "Command has expired"}, "command_result_conflict": {409, "Command result conflicts with stored result"},
	"batch_conflict": {409, "Batch identifier was used with different content"}, "event_id_conflict": {409, "Event identifier was used with different content"},
	"ingestion_pending": {503, "Telemetry persistence is not yet confirmed"}, "ingestion_busy": {503, "Another telemetry batch is in progress"}, "telemetry_unavailable": {503, "Telemetry delivery is temporarily unavailable"},
	"precondition_required": {428, "A precondition header is required"}, "rate_limited": {429, "Too many requests"}, "internal_error": {500, "Internal server error"}, "database_unavailable": {503, "Database is temporarily unavailable"}, "service_unavailable": {503, "Service is temporarily unavailable"},
}

func NewError(code string) *Error {
	entry, ok := errorsByCode[code]
	if !ok {
		code = "internal_error"
		entry = errorsByCode[code]
	}
	return &Error{Code: code, Message: entry.message, Status: entry.status}
}
func Fail(c *gin.Context, e *Error) {
	copy := *e
	if copy.Status != 400 && copy.Status != 422 {
		copy.Fields = nil
	}
	if len(copy.Fields) > MaxFieldErrors {
		copy.Fields = copy.Fields[:MaxFieldErrors]
	}
	if copy.Status == 429 || copy.Status == 503 {
		if c.Writer.Header().Get("Retry-After") == "" {
			c.Header("Retry-After", "1")
		}
	}
	c.AbortWithStatusJSON(copy.Status, ErrorResponse{&copy, ID(c.Writer.Header().Get("X-Request-ID"))})
}
