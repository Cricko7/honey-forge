package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckRequestBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, path, payload string
		valid               bool
	}{
		{"endpoint", "/pipeline", `{"count":3}`, true},
		{"absolute URL", "http://localhost:8080/", `{}`, false},
		{"host", "//localhost/", `{}`, false},
		{"query", "/pipeline?secret=x", `{}`, false},
		{"malformed JSON", "/pipeline", `{`, false},
		{"oversized JSON", "/pipeline", `"` + strings.Repeat("x", MaxPayloadBytes) + `"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Check(Request{Path: tc.path, Payload: json.RawMessage(tc.payload), Service: "web", SourceIP: "127.0.0.1"})
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}
