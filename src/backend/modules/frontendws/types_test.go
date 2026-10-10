package frontendws

import (
	"testing"

	"honey-forge/internal/contract"
)

func TestSubscribe(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		valid     bool
	}{
		{"live", `{"after":null}`, true}, {"replay", `{"after":"opaque"}`, true},
		{"missing", `{}`, false}, {"number", `{"after":2}`, false}, {"empty", `{"after":""}`, false},
		{"organization injection", `{"after":null,"organization_id":"foreign"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, err := contract.DecodeEnvelope([]byte(`{"message_id":"11111111-1111-4111-8111-111111111111","type":"stream.subscribe","payload":` + tt.raw + `}`))
			if err != nil {
				t.Fatal(err)
			}
			_, err = subscribe(e)
			if (err == nil) != tt.valid {
				t.Fatalf("error %v", err)
			}
		})
	}
}
