package contract

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

// present is a binding rule for mandatory values whose zero value is valid.
// It also works with Nullable, where a present null differs from an absent key.
func missingPresent(b []byte, t reflect.Type, path string) []FieldError {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	fields := []FieldError{}
	switch t.Kind() {
	case reflect.Struct:
		var raw map[string]json.RawMessage
		if json.Unmarshal(b, &raw) != nil {
			return nil
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			value, exists := raw[name]
			childPath := path + "/" + escapePointer(name)
			if !exists {
				for _, rule := range strings.Split(f.Tag.Get("validate"), ",") {
					if rule == "present" {
						fields = append(fields, FieldError{Path: childPath, Code: "required", Message: "Field is required"})
					}
				}
			} else {
				fields = append(fields, missingPresent(value, f.Type, childPath)...)
			}
			if len(fields) >= MaxFieldErrors {
				return fields[:MaxFieldErrors]
			}
		}
	case reflect.Array, reflect.Slice:
		var array []json.RawMessage
		if json.Unmarshal(b, &array) != nil {
			return nil
		}
		for i, v := range array {
			fields = append(fields, missingPresent(v, t.Elem(), path+"/"+strconv.Itoa(i))...)
			if len(fields) >= MaxFieldErrors {
				return fields[:MaxFieldErrors]
			}
		}
	}
	return fields
}
