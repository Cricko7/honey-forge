package main

import "testing"

func TestAPIKeyRateLimit(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        int
		invalid     bool
	}{
		{"unset", "", 60, false},
		{"configured", "120", 120, false},
		{"zero", "0", 0, true},
		{"negative", "-1", 0, true},
		{"malformed", "sixty", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("API_KEY_RATE_LIMIT_PER_MINUTE", tc.value)

			got, err := apiKeyRateLimit()
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("got %d, %v; want %d, invalid %v", got, err, tc.want, tc.invalid)
			}
		})
	}
}
