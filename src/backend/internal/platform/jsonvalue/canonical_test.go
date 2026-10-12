package jsonvalue

import (
	"encoding/json"
	"testing"
)

func TestCanonical(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"key order and nested numbers", `{"b":[1.00,100,-0],"a":{"x":0.10}}`, `{"a":{"x":1e-1},"b":[1e0,1e2,0]}`},
		{"large integer stays exact", `{"n":9007199254740993}`, `{"n":9007199254740993e0}`},
		{"huge exponent stays compact", `{"n":1e999999999999999999999}`, `{"n":1e999999999999999999999}`},
		{"negative fraction", `{"n":-123.4500e-10}`, `{"n":-12345e-12}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Canonical(json.RawMessage(tc.input))
			if err != nil || string(got) != tc.want {
				t.Fatalf("canonical = %s, %v; want %s", got, err, tc.want)
			}
		})
	}

	if _, err := Canonical(make(chan int)); err == nil {
		t.Fatal("accepted a non-JSON value")
	}
}
