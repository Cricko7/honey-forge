package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

var setup sync.Once

// Configure must run before serving requests. Validation uses the contract's validate tags.
func Configure() {
	setup.Do(func() {
		binding.EnableDecoderDisallowUnknownFields = true
		binding.EnableDecoderUseNumber = true
		v := binding.Validator.Engine().(*validator.Validate)
		v.SetTagName("validate")
		v.RegisterTagNameFunc(func(f reflect.StructField) string { return strings.Split(f.Tag.Get("json"), ",")[0] })
		for name, fn := range map[string]validator.Func{
			"present": func(f validator.FieldLevel) bool {
				value, ok := f.Field().Interface().(interface{ Present() bool })
				return !ok || value.Present()
			},
			"name":    func(f validator.FieldLevel) bool { return ValidName(f.Field().String()) },
			"type_id": func(f validator.FieldLevel) bool { return typePattern.MatchString(f.Field().String()) },
		} {
			if err := v.RegisterValidation(name, fn); err != nil {
				panic(err)
			}
		}
	})
}

func Middleware() gin.HandlerFunc {
	Configure()
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(WithHTTPMethod(c.Request.Context(), c.Request.Method))
		c.Header("X-Request-ID", string(NewID()))
		c.Header("Cache-Control", "no-store")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes)
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(c.Request.Context(), "request panicked", "request_id", c.Writer.Header().Get("X-Request-ID"))
				if !c.Writer.Written() {
					Fail(c, NewError("internal_error"))
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

// BindJSON checks the bounded JSON document before Gin binding and declarative validation.
func BindJSON(c *gin.Context, dst any) bool {
	b, ok := readBody(c)
	if !ok {
		return false
	}
	if err := CheckJSON(b); err != nil {
		Fail(c, NewError("invalid_json"))
		return false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil || !strictShape(b, reflect.TypeOf(dst)) {
		Fail(c, NewError("invalid_json"))
		return false
	}
	if fields := missingPresent(b, reflect.TypeOf(dst), ""); len(fields) > 0 {
		e := NewError("validation_failed")
		e.Fields = fields
		Fail(c, e)
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(b))
	if err := c.ShouldBindJSON(dst); err != nil {
		var domain *Error
		if errors.As(err, &domain) {
			Fail(c, domain)
			return false
		}
		var validation validator.ValidationErrors
		if errors.As(err, &validation) {
			e := NewError("validation_failed")
			for _, field := range validation {
				if len(e.Fields) == MaxFieldErrors {
					break
				}
				e.Fields = append(e.Fields, FieldError{Path: fieldPointer(field.Namespace()), Code: "invalid_value", Message: "Value does not satisfy the field constraints"})
			}
			Fail(c, e)
		} else {
			Fail(c, NewError("invalid_json"))
		}
		return false
	}
	return true
}

func fieldPointer(namespace string) string {
	parts := strings.Split(namespace, ".")
	if len(parts) > 1 {
		parts = parts[1:]
	}
	for i, p := range parts {
		p = strings.ReplaceAll(strings.ReplaceAll(p, "~", "~0"), "/", "~1")
		p = strings.ReplaceAll(strings.ReplaceAll(p, "[", "/"), "]", "")
		parts[i] = p
	}
	return "/" + strings.Join(parts, "/")
}

// CheckJSON rejects invalid UTF-8, duplicate keys at every depth and trailing documents.
func CheckJSON(b []byte) error {
	if !utf8.Valid(b) {
		return errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := scanValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func scanValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return errors.New("duplicate or invalid object key")
			}
			seen[s] = true
			if err := scanValue(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := scanValue(d); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected delimiter")
	}
	_, err = d.Token()
	return err
}
