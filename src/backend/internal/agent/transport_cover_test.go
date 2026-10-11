package agent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionResourceHandshake(t *testing.T) {
	for _, tt := range []struct {
		name, contentType string
		status            int
		revoked           bool
	}{
		{"public asset after rejected credentials", "application/javascript; charset=utf-8", 200, true},
		{"misrouted website", "text/html", 200, false},
		{"temporary outage", "text/plain", 503, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
			}))
			defer server.Close()
			journal, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "cover-test", 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			config := server.Client().Transport.(*http.Transport).TLSClientConfig
			err = session(t.Context(), Options{URL: "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream", Token: "secret", TLS: config}, journal, NewService(journal, nil), nil, nil)
			if err == nil || errors.Is(err, ErrRevoked) != tt.revoked {
				t.Fatalf("revoked=%v error=%v", tt.revoked, err)
			}
		})
	}
}
