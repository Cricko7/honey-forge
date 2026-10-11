package main

import "testing"

func TestAPIAddressUsesLoopbackByDefault(t *testing.T) {
	for _, tt := range []struct{ name, configured, want string }{
		{"local default", "", "127.0.0.1:8443"},
		{"explicit mgmt IP", "10.10.0.2:8443", "10.10.0.2:8443"},
		{"private container listener", ":8443", ":8443"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("API_ADDR", tt.configured)
			if got := apiAddress(); got != tt.want {
				t.Fatalf("address=%q want=%q", got, tt.want)
			}
		})
	}
}
