package httpx

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"honey-forge/internal/platform/validation"
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
	return validation.Fields(fields, typ, prefix)
}

func FieldPath(namespace string, typ reflect.Type) string {
	return validation.FieldPath(namespace, typ)
}
