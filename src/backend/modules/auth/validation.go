package auth

import (
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

var (
	validationOnce sync.Once
	validationErr  error
)

func registerValidationRules() error {
	validationOnce.Do(func() {
		binding.EnableDecoderDisallowUnknownFields = true

		engine, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			validationErr = errors.New("Gin validator engine is unavailable")
			return
		}

		rules := map[string]validator.Func{
			"orgname": func(fl validator.FieldLevel) bool {
				size := utf8.RuneCountInString(strings.TrimSpace(fl.Field().String()))

				return size >= 1 && size <= 100
			},
			"operatoremail": func(fl validator.FieldLevel) bool {
				value := strings.TrimSpace(fl.Field().String())
				if len(value) > 254 || strings.ContainsFunc(value, func(r rune) bool { return r > 127 }) {
					return false
				}

				address, err := mail.ParseAddress(value)

				return err == nil && address.Name == "" && address.Address == value
			},
			"joincode": func(fl validator.FieldLevel) bool {
				return validOpaque(fl.Field().String(), 32)
			},
			"maxbytes": func(fl validator.FieldLevel) bool {
				maximum, err := strconv.Atoi(fl.Param())

				return err == nil && len(fl.Field().String()) <= maximum
			},
		}

		for tag, rule := range rules {
			if err := engine.RegisterValidation(tag, rule); err != nil {
				validationErr = fmt.Errorf("registering %s validator: %w", tag, err)
				return
			}
		}
	})

	return validationErr
}
