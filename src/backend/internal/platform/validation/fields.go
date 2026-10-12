// Package validation describes field failures independently of HTTP handling.
package validation

import (
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

type FieldError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Fields(fields validator.ValidationErrors, typ reflect.Type, prefix string) []FieldError {
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
