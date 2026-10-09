package httpx

import (
	"reflect"
	"testing"
)

func TestStrictObject(t *testing.T) {
	type input struct {
		Name   string         `json:"name"`
		Config map[string]any `json:"config"`
	}
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"valid", `{"name":"demo","config":{"nested":[{"a":1}]}}`, true},
		{"unknown envelope", `{"other":1}`, false},
		{"case-sensitive keys", `{"Name":"demo"}`, false},
		{"duplicate envelope", `{"name":"a","name":"b"}`, false},
		{"nested duplicate", `{"config":{"nested":[{"a":1,"a":2}]}}`, false},
		{"null envelope", `{"name":null}`, false},
		{"dynamic null allowed", `{"config":{"nullable":null}}`, true},
		{"array config", `{"config":[]}`, false},
		{"two documents", `{} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := StrictObject([]byte(tc.raw), reflect.TypeFor[input]()); got != tc.valid {
				t.Fatalf("valid = %v, want %v", got, tc.valid)
			}
		})
	}
}
