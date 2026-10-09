package httpx

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

const MaxBodySize = 256 << 10

func BodyLimit(c *gin.Context) {
	if c.Request.ContentLength > MaxBodySize {
		WriteError(c, 413, "body_too_large", "Request body is too large")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodySize)
	c.Next()
}

func ReadBody(c *gin.Context) ([]byte, bool) {
	raw, err := io.ReadAll(c.Request.Body)
	if err == nil {
		return raw, true
	}
	var large *http.MaxBytesError
	if errors.As(err, &large) {
		WriteError(c, 413, "body_too_large", "Request body is too large")
	} else {
		WriteError(c, 400, "invalid_json", "Invalid JSON body")
	}
	return nil, false
}

func EmptyBody(c *gin.Context) bool {
	raw, ok := ReadBody(c)
	if ok && len(bytes.TrimSpace(raw)) != 0 {
		WriteError(c, 400, "invalid_json", "Invalid JSON body")
		return false
	}
	return ok
}

// shape is an optional feature-specific check of mutually exclusive fields.
func BindJSON(c *gin.Context, dst any, shape func([]byte) bool) bool {
	raw, ok := ReadBody(c)
	if !ok {
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		WriteError(c, 400, "invalid_json", "Invalid JSON body")
		return false
	}
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		WriteError(c, 415, "unsupported_media_type", "Expected application/json")
		return false
	}
	if !utf8.Valid(raw) || !StrictObject(raw, reflect.TypeOf(dst).Elem()) || shape != nil && !shape(raw) {
		WriteError(c, 400, "invalid_json", "Invalid JSON body")
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if err := c.ShouldBindJSON(dst); err != nil {
		var fields validator.ValidationErrors
		if errors.As(err, &fields) {
			WriteError(c, 422, "validation_failed", "Request validation failed", ValidationFields(fields, reflect.TypeOf(dst).Elem(), "")...)
		} else {
			WriteError(c, 400, "invalid_json", "Invalid JSON body")
		}
		return false
	}
	return true
}

func ValidationFields(fields validator.ValidationErrors, typ reflect.Type, prefix string) []FieldError {
	details := make([]FieldError, 0, min(len(fields), 20))
	for _, f := range fields {
		details = append(details, FieldError{Path: prefix + FieldPath(f.StructNamespace(), typ), Code: f.Tag(), Message: validationMessage(f.Tag(), f.Param())})
		if len(details) == 20 {
			break
		}
	}
	return details
}

func validationMessage(rule, param string) string {
	switch rule {
	case "required", "required_if":
		return "Value is required"
	case "excluded_if":
		return "Field is not allowed for this organization mode"
	case "min":
		return "Value must contain at least " + param + " characters or meet the minimum"
	case "max":
		return "Value exceeds the maximum of " + param
	case "maxbytes":
		return "Value must not exceed " + param + " UTF-8 bytes"
	case "operatoremail":
		return "Expected a valid ASCII email without display name, up to 254 characters after trimming"
	case "orgname", "profilename":
		return "Name must contain 1 to 100 characters after trimming"
	case "joincode":
		return "Join code must contain exactly 32 base64url characters"
	default:
		return "Value is invalid"
	}
}

func FieldPath(namespace string, typ reflect.Type) string {
	parts := strings.Split(namespace, ".")[1:]
	var names []string
	for _, part := range parts {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		name, index, indexed := strings.Cut(part, "[")
		field, ok := typ.FieldByName(name)
		if !ok {
			break
		}
		names = append(names, strings.Split(field.Tag.Get("json"), ",")[0])
		typ = field.Type
		if indexed {
			names = append(names, strings.TrimSuffix(index, "]"))
			typ = typ.Elem()
		}
	}
	return "/" + strings.Join(names, "/")
}
