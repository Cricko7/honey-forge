package agent

import (
	"strings"
	"testing"
)

func TestRunRejectsPartialWorkerIsolationBeforeOpeningStorage(t *testing.T) {
	for _, tt := range []struct{ name, image, network string }{{"missing network", "image", ""}, {"missing image", "", "decoys"}, {"host networking", "image", "host"}, {"no decoy interface", "image", "none"}} {
		t.Run(tt.name, func(t *testing.T) {
			err := Run(t.Context(), Options{WorkerImage: tt.image, WorkerNetwork: tt.network})
			if err == nil || !strings.Contains(err.Error(), "NM_WORKER_IMAGE") {
				t.Fatalf("isolation configuration silently fell back: %v", err)
			}
		})
	}
}

func TestRunRejectsUnsafeBackendURL(t *testing.T) {
	for _, tt := range []struct{ name, url string }{
		{"plaintext WebSocket", "ws://backend.example/assets/stream"},
		{"plaintext HTTP", "http://backend.example/assets/stream"},
		{"HTTPS without WebSocket", "https://backend.example/assets/stream"},
		{"missing host", "wss:///assets/stream"},
		{"credentials in URL", "wss://user:secret@backend.example/assets/stream"},
		{"query token", "wss://backend.example/assets/stream?token=secret"},
		{"fragment", "wss://backend.example/assets/stream#secret"},
		{"wrong path", "wss://backend.example/api/stream"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := Run(t.Context(), Options{URL: tt.url})
			if err == nil || !strings.Contains(err.Error(), "NM_WS_URL must be wss://host/assets/stream") {
				t.Fatalf("unsafe URL must be rejected before opening storage: %v", err)
			}
		})
	}
}
