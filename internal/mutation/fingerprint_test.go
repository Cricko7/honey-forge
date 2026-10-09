package mutation

import (
	"encoding/json"
	"testing"
)

func TestNormalizedNumbers(t *testing.T) {
	for _, tt := range []struct{ name, a, b string }{{"integer", `{"x":1}`, `{"x":1.0}`}, {"exponent", `{"x":100}`, `{"x":1e2}`}, {"fraction", `{"x":0.10}`, `{"x":1e-1}`}, {"signed zero", `{"x":-0}`, `{"x":0}`}} {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Fingerprint(json.RawMessage(tt.a))
			if err != nil {
				t.Fatal(err)
			}
			b, err := Fingerprint(json.RawMessage(tt.b))
			if err != nil {
				t.Fatal(err)
			}
			if a != b {
				t.Fatal("equivalent normalized JSON differs")
			}
		})
	}
}
