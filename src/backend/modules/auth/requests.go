package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
)

func bindRequest(c *gin.Context, dst any) bool {
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "application/json" {
		httpx.WriteError(c, 415, "unsupported_media_type", "Expected application/json")
		return false
	}

	raw, ok := readBody(c)
	if !ok {
		return false
	}

	if !utf8.Valid(raw) || !json.Valid(raw) || !strictObject(raw, reflect.TypeOf(dst).Elem()) {
		httpx.WriteError(c, 400, "invalid_json", "Invalid JSON body")
		return false
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if err := c.ShouldBindJSON(dst); err != nil {
		var fields validator.ValidationErrors
		if errors.As(err, &fields) {
			details := make([]httpx.FieldError, 0, min(len(fields), 20))
			for _, violation := range fields {
				path := fieldPath(violation.StructNamespace(), reflect.TypeOf(dst).Elem())
				details = append(details, httpx.FieldError{
					Path: path, Code: violation.Tag(), Message: validationMessage(violation.Tag(), violation.Param()),
				})
				if len(details) == 20 {
					break
				}
			}

			httpx.WriteError(c, 422, "validation_failed", "Request validation failed", details...)
			return false
		}

		httpx.WriteError(c, 400, "invalid_json", "Invalid JSON body")
		return false
	}

	return true
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
	case "orgname":
		return "Name must contain 1 to 100 characters after trimming"
	case "joincode":
		return "Join code must contain exactly 32 base64url characters"
	default:
		return "Value is invalid"
	}
}

func fieldPath(namespace string, typ reflect.Type) string {
	parts := strings.Split(namespace, ".")[1:]
	var names []string

	for _, part := range parts {
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		field, ok := typ.FieldByName(part)
		if !ok {
			break
		}

		names = append(names, strings.Split(field.Tag.Get("json"), ",")[0])
		typ = field.Type
	}

	return "/" + strings.Join(names, "/")
}

func bodyLimit(c *gin.Context) {
	if c.Request.ContentLength > maxBodySize {
		httpx.WriteError(c, 413, "body_too_large", "Request body is too large")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)
	c.Next()
}

func readBody(c *gin.Context) ([]byte, bool) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			httpx.WriteError(c, 413, "body_too_large", "Request body is too large")
		} else {
			httpx.WriteError(c, 400, "invalid_json", "Invalid JSON body")
		}

		return nil, false
	}

	return raw, true
}

func emptyBody(c *gin.Context) bool {
	raw, ok := readBody(c)
	if !ok {
		return false
	}
	if len(bytes.TrimSpace(raw)) != 0 {
		httpx.WriteError(c, 400, "invalid_json", "Invalid JSON body")
		return false
	}

	return true
}
