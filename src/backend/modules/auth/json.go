package auth

import (
	"encoding/json"
	"honey-forge/src/backend/internal/platform/httpx"
	"reflect"
)

func StrictObject(raw []byte, typ reflect.Type) bool {
	if !httpx.StrictObject(raw, typ) {
		return false
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return false
	}
	if typ == reflect.TypeFor[RegisterRequest]() {
		if org, ok := values["organization"]; ok && !StrictObject(org, reflect.TypeFor[OrganizationInput]()) {
			return false
		}
	}
	if typ == reflect.TypeFor[OrganizationInput]() {
		var mode string
		if raw, ok := values["mode"]; ok {
			if err := json.Unmarshal(raw, &mode); err != nil {
				return false
			}
		}
		_, hasCode := values["join_code"]
		_, hasName := values["name"]
		if mode == "create" && hasCode || mode == "join" && hasName {
			return false
		}
	}
	return true
}
