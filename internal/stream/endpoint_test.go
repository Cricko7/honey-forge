package stream

import "testing"

func TestAgentEndpoint(t *testing.T) {
	endpoint, err := NewAgentEndpoint("https://control.example", "/assets/stream")
	if err != nil || endpoint.URL() != "wss://control.example/assets/stream" {
		t.Fatalf("%+v %v", endpoint, err)
	}
	for _, tt := range []struct{ name, origin, path string }{{"plain HTTP", "http://control.example", "/assets/stream"}, {"token in URL", "https://token@control.example", "/assets/stream"}, {"query token", "https://control.example", "/assets/stream?token=secret"}, {"path traversal", "https://control.example", "/assets/../stream"}} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewAgentEndpoint(tt.origin, tt.path); err == nil {
				t.Fatal("unsafe endpoint accepted")
			}
		})
	}
}
