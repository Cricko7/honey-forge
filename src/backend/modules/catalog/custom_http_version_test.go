package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

func TestCustomHTTPPublishedVersionRemainsImmutable(t *testing.T) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, customHTTPJSON); err != nil {
		t.Fatal(err)
	}

	const published = "e86cf5f5bbf7aafa3d16a5bc75685618885d94146f086b208ac6e49addb9979d"
	if got := fmt.Sprintf("%x", sha256.Sum256(compact.Bytes())); got != published {
		t.Fatalf("custom-http/1 changed: %s", got)
	}

	s := testService(t)
	if _, err := s.LookupType(t.Context(), "custom-http", 2); err != nil {
		t.Fatal(err)
	}

	event := json.RawMessage(`{"service":"web","node_id":"notify","name":"stage","severity":"high","payload":{}}`)
	wantCode(t, s.CheckEvent(t.Context(), "custom-http", 1, "http.custom", event), "telemetry_invalid")
	wantCode(t, s.CheckEvent(t.Context(), "custom-http", 2, "http.custom", event), "")
}

func TestCustomHTTPPipelineVersionRemainsImmutable(t *testing.T) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, customHTTPPipelineJSON); err != nil {
		t.Fatal(err)
	}

	const published = "225666095a80dde4a15ad417784721162d4c8f51bca886722329d6c6854cea51"
	if got := fmt.Sprintf("%x", sha256.Sum256(compact.Bytes())); got != published {
		t.Fatalf("custom-http/2 changed: %s", got)
	}
}

func TestEmbeddedHoneytokensUseBuiltinValidation(t *testing.T) {
	for _, tc := range []struct {
		name, tokens string
		valid        bool
	}{
		{"valid", `[{"id":"report","kind":"url","path":"/report","value":"report"}]`, true},
		{"empty", `[]`, false},
		{"short key", `[{"id":"key","kind":"key","path":"/secret","value":"short"}]`, false},
		{"reserved path", `[{"id":"report","kind":"url","path":"/artifacts/report","value":"report"}]`, false},
		{"duplicate", `[{"id":"report","kind":"url","path":"/report","value":"a"},{"id":"report","kind":"url","path":"/another","value":"b"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkEmbeddedTokens(tc.tokens); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}
