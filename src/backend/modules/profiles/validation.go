package profiles

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"honey-forge/internal/platform/httpx"
)

var typePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

var requestValidator = newRequestValidator()

var validationOnce sync.Once

var validationErr error

func rules() map[string]validator.Func {
	return map[string]validator.Func{
		"profilename": func(fl validator.FieldLevel) bool {
			n := utf8.RuneCountInString(strings.TrimSpace(fl.Field().String()))
			return n >= 1 && n <= 100
		},
		"profiletype":    func(fl validator.FieldLevel) bool { return typePattern.MatchString(fl.Field().String()) },
		"profilepointer": func(fl validator.FieldLevel) bool { _, ok := pointerParts(fl.Field().String()); return ok },
	}
}

func newRequestValidator() *validator.Validate {
	v := validator.New()
	for tag, rule := range rules() {
		if err := v.RegisterValidation(tag, rule); err != nil {
			panic(err)
		}
	}

	return v
}

func RegisterValidation() error {
	validationOnce.Do(func() {
		binding.EnableDecoderDisallowUnknownFields = true
		binding.EnableDecoderUseNumber = true
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			validationErr = errors.New("Gin validator engine is unavailable")
			return
		}

		for tag, rule := range rules() {
			if err := v.RegisterValidation(tag, rule); err != nil {
				validationErr = fmt.Errorf("registering %s: %w", tag, err)
				return
			}
		}
	})
	return validationErr
}

func ValidateRequest(req any) error {
	if err := requestValidator.Struct(req); err != nil {
		var fields validator.ValidationErrors
		if errors.As(err, &fields) {
			return &ValidationError{ErrValidation, httpx.ValidationFields(fields, reflect.TypeOf(req), "")}
		}

		return fmt.Errorf("validating profile request: %w", err)
	}

	return nil
}

func ValidID(id string) bool { return requestValidator.Var(id, "required,uuid") == nil }

func ValidTypeID(value string) bool { return typePattern.MatchString(value) }
