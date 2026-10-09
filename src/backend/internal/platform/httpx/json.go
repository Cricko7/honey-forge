package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
)

// StrictObject checks the envelope; dynamic objects are validated by their catalog.
func StrictObject(raw []byte, typ reflect.Type) bool {
	if !json.Valid(raw) || !uniqueKeys(json.NewDecoder(bytes.NewReader(raw))) {
		return false
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	known := make(map[string]reflect.Type)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		known[strings.Split(f.Tag.Get("json"), ",")[0]] = f.Type
	}
	for name, value := range fields {
		ft, ok := known[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Struct:
			if !StrictObject(value, ft) {
				return false
			}
		case reflect.Map:
			if len(value) == 0 || bytes.TrimSpace(value)[0] != '{' {
				return false
			}
		case reflect.Slice:
			if ft.Elem().Kind() == reflect.Struct {
				var items []json.RawMessage
				if json.Unmarshal(value, &items) != nil {
					return false
				}
				for _, item := range items {
					if !StrictObject(item, ft.Elem()) {
						return false
					}
				}
			}
		}
	}
	return true
}

func uniqueKeys(d *json.Decoder) bool {
	token, err := d.Token()
	if err != nil {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return true
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return false
			}
			seen[name] = true
			if !uniqueKeys(d) {
				return false
			}
		}
	case '[':
		for d.More() {
			if !uniqueKeys(d) {
				return false
			}
		}
	default:
		return false
	}
	_, err = d.Token()
	return err == nil
}

func DecodeObject(raw []byte) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var object map[string]any
	if err := d.Decode(&object); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, &json.SyntaxError{}
	}
	return object, nil
}
