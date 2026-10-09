package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
	"github.com/Cricko7/honey-forge/src/backend/modules/profiles"
	"github.com/go-playground/validator/v10"
)

// The module 03 adapter currently supports its specified immutable tcp-banner/1 config.
func lookupProfileType(ctx context.Context, id string, version int32) (profiles.Type, error) {
	if err := ctx.Err(); err != nil {
		return profiles.Type{}, err
	}
	if id != "tcp-banner" || version != 1 {
		return profiles.Type{}, profiles.ErrUnknownType
	}
	return profiles.Type{InteractionLevel: "low", AvailableForNewProfiles: true, CheckConfig: checkTCPConfig}, nil
}

type tcpConfigInput struct {
	Listeners  []tcpListener  `json:"listeners" validate:"required,min=1,max=16,dive"`
	Logging    *tcpLogging    `json:"logging" validate:"required"`
	Management *tcpManagement `json:"management" validate:"required"`
}
type tcpListener struct {
	Name             string `json:"name" validate:"required,listenername"`
	Port             int64  `json:"port" validate:"min=1,max=65535"`
	Banner           string `json:"banner" validate:"required,max=4096,maxbytes=4096"`
	CloseAfterBanner *bool  `json:"close_after_banner" validate:"required"`
}
type tcpLogging struct {
	CapturePayload  *bool  `json:"capture_payload" validate:"required"`
	MaxPayloadBytes *int64 `json:"max_payload_bytes" validate:"required,min=0,max=4096"`
}
type tcpManagement struct {
	Heartbeat *int64 `json:"heartbeat_interval_seconds" validate:"required,min=5,max=10"`
	Flush     *int64 `json:"telemetry_flush_interval_ms" validate:"required,min=100,max=1000"`
}

var tcpValidator = newTCPValidator()

func newTCPValidator() *validator.Validate {
	v := validator.New()
	pattern := regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	for name, rule := range map[string]validator.Func{
		"listenername": func(fl validator.FieldLevel) bool { return pattern.MatchString(fl.Field().String()) },
		"maxbytes": func(fl validator.FieldLevel) bool {
			max, err := strconv.Atoi(fl.Param())
			return err == nil && len(fl.Field().String()) <= max
		},
	} {
		if err := v.RegisterValidation(name, rule); err != nil {
			panic(err)
		}
	}
	return v
}
func checkTCPConfig(ctx context.Context, c profiles.Object) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Draft 2020-12 integers also accept 2222.0 and 2222e0; normalize before typed decoding.
	raw, err := json.Marshal(tcpNumbers(c))
	if err != nil {
		return fmt.Errorf("encoding TCP config: %w", err)
	}
	if !httpx.StrictObject(raw, reflect.TypeFor[tcpConfigInput]()) {
		return configFields("/config", "invalid", "Configuration does not match the type schema")
	}
	var input tcpConfigInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return configFields("/config", "type", "Configuration does not match the type schema")
	}
	if err := tcpValidator.Struct(input); err != nil {
		var fields validator.ValidationErrors
		if errors.As(err, &fields) {
			return &profiles.ValidationError{Cause: profiles.ErrConfigInvalid, Fields: httpx.ValidationFields(fields, reflect.TypeFor[tcpConfigInput](), "/config")}
		}
		return fmt.Errorf("validating TCP config: %w", err)
	}
	names := map[string]bool{}
	ports := map[int64]bool{}
	for i, l := range input.Listeners {
		if names[l.Name] {
			return configFields(fmt.Sprintf("/config/listeners/%d/name", i), "unique", "Listener names must be unique")
		}
		if ports[l.Port] {
			return configFields(fmt.Sprintf("/config/listeners/%d/port", i), "unique", "Listener ports must be unique")
		}
		names[l.Name] = true
		ports[l.Port] = true
	}
	if !*input.Logging.CapturePayload && *input.Logging.MaxPayloadBytes != 0 || *input.Logging.CapturePayload && *input.Logging.MaxPayloadBytes == 0 {
		return configFields("/config/logging/max_payload_bytes", "out_of_range", "Value is inconsistent with capture_payload")
	}
	return nil
}
func configFields(path, code, message string) error {
	return &profiles.ValidationError{Cause: profiles.ErrConfigInvalid, Fields: []httpx.FieldError{{Path: path, Code: code, Message: message}}}
}
func tcpNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		raw, err := httpx.CanonicalJSON(x)
		if err != nil {
			return x
		}
		digits, exponent, exponential := strings.Cut(string(raw), "e")
		if !exponential {
			return json.Number(digits)
		}
		power, err := strconv.Atoi(exponent)
		if err != nil || power < 0 || power > 18 || len(digits)+power > 19 {
			return x
		}
		return json.Number(digits + strings.Repeat("0", power))
	case map[string]any:
		m := make(profiles.Object, len(x))
		for k, v := range x {
			m[k] = tcpNumbers(v)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, v := range x {
			a[i] = tcpNumbers(v)
		}
		return a
	default:
		return x
	}
}
