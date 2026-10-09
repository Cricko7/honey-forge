package auth

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// Check exact keys, duplicate keys and nulls before Gin's typed binding.
func strictObject(raw []byte, typ reflect.Type) bool {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return false
	}

	fields := make(map[string]reflect.Type)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fields[strings.Split(field.Tag.Get("json"), ",")[0]] = field.Type
	}

	seen := make(map[string]bool)
	values := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return false
		}

		name, ok := token.(string)
		fieldType, known := fields[name]
		if !ok || !known || seen[name] {
			return false
		}

		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
		values[name] = value

		if fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() == reflect.Struct && !strictObject(value, fieldType) {
			return false
		}
	}

	if typ == reflect.TypeFor[OrganizationInput]() {
		var mode string
		if value, ok := values["mode"]; ok {
			if err := json.Unmarshal(value, &mode); err != nil {
				return false
			}
		}

		if mode == "create" && seen["join_code"] || mode == "join" && seen["name"] {
			return false
		}
	}

	return true
}
