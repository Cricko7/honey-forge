package honeytrap

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"honey-forge/modules/events"
)

func TestMaskErrors(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		status             int
	}{
		{"missing", "GET", "/missing", 404},
		{"method", "POST", "/missing", 405},
		{"unauthorized", "GET", "/v1/backups", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := testRuntime(t, func(context.Context, events.AgentEvent) error {
				return nil
			})
			w := httptest.NewRecorder()
			r.serveHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || w.Header().Get("Server") != "nginx" || !strings.Contains(w.Body.String(), "<html>") {
				t.Fatalf("unrealistic error: %d %v %q", w.Code, w.Header(), w.Body.String())
			}

			if tc.status == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing authentication challenge")
			}
		})
	}
}
