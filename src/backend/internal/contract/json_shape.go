package contract

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// Pointer fields represent optional/nullable values. Dynamic maps are schema-validated by their feature.
func strictShape(b []byte, t reflect.Type) bool {
	if t == nil {
		return false
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		if t.Implements(reflect.TypeFor[interface{ AllowsNull() bool }]()) {
			return reflect.Zero(t).Interface().(interface{ AllowsNull() bool }).AllowsNull()
		}
		return t.Kind() == reflect.Pointer || t.Kind() == reflect.Interface
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return true
	}
	switch t.Kind() {
	case reflect.Struct:
		var raw map[string]json.RawMessage
		if json.Unmarshal(b, &raw) != nil || raw == nil {
			return false
		}
		fields := map[string]reflect.Type{}
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
			fields[name] = f.Type
		}
		for name, v := range raw {
			ft, ok := fields[name]
			if !ok || !strictShape(v, ft) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		var raw []json.RawMessage
		if json.Unmarshal(b, &raw) != nil {
			return false
		}
		for _, v := range raw {
			if !strictShape(v, t.Elem()) {
				return false
			}
		}
	}
	return true
}
