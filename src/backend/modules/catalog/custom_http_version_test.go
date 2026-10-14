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
