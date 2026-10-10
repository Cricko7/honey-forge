package honeytrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func testSnapshot() profiles.Snapshot {
	return profiles.Snapshot{TypeID: "honeytoken-http", TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{
		"services": []any{map[string]any{"name": "web", "port": 8080}},
		"tokens": []any{
			map[string]any{"id": "backup", "kind": "key", "path": "/v1/backups", "value": "fake-key-0123456789"},
			map[string]any{"id": "document", "kind": "file", "path": "/backup.txt", "value": "fake backup"},
			map[string]any{"id": "report", "kind": "url", "path": "/reports/latest", "value": "report ready"},
		},
		"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
	}}
}

func TestHoneytokenRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, key string
		status                  int
		trigger                 bool
	}{
		{"key", "GET", "/v1/backups", "fake-key-0123456789", 200, true},
		{"wrong key", "GET", "/v1/backups", "wrong", 401, false},
		{"missing key", "GET", "/v1/backups", "", 401, false},
		{"duplicate authorization", "GET", "/v1/backups", "fake-key-0123456789", 401, false},
		{"file", "GET", "/backup.txt", "", 200, true},
		{"url", "GET", "/reports/latest", "", 200, true},
		{"distribution", "GET", "/artifacts/backup", "", 200, false},
		{"unknown", "GET", "/unknown", "", 404, false},
		{"wrong method", "POST", "/reports/latest", "", 405, false},
		{"head", "HEAD", "/reports/latest", "", 405, false},
		{"encoded alias", "GET", "/%62ackup.txt", "", 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var collected []events.AgentEvent
			r := testRuntime(t, func(_ context.Context, e events.AgentEvent) error { collected = append(collected, e); return nil })
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Header.Set("X-Forwarded-For", "203.0.113.2")
			if tc.key != "" {
				request.Header.Set("Authorization", "Bearer "+tc.key)
			}
			if tc.name == "duplicate authorization" {
				request.Header.Add("Authorization", "Bearer "+tc.key)
			}
			w := httptest.NewRecorder()
			r.serveHTTP(w, request)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			found := false
			for i, e := range collected {
				if e.SessionSequence != int64(i+1) || e.SessionID != collected[0].SessionID || e.Destination.Port != 8080 || e.Source.IP != "192.0.2.1" {
					t.Fatalf("invalid envelope: %+v", e)
				}
				if strings.Contains(string(e.Data), "fake-key") {
					t.Fatal("key leaked into telemetry")
				}
				if e.EventType == "honeytoken.triggered" {
					found = true
					var data map[string]any
					if err := json.Unmarshal(e.Data, &data); err != nil {
						t.Fatal(err)
					}
					if data["method"] != "GET" || data["token_id"] == nil {
						t.Fatal(data)
					}
				}
			}
			if found != tc.trigger {
				t.Fatalf("trigger=%v want %v", found, tc.trigger)
			}
			if len(collected) < 2 || collected[0].EventType != "service.connection_opened" || collected[len(collected)-1].EventType != "service.connection_closed" {
				t.Fatal("incomplete session")
			}
			if tc.name == "file" && !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
				t.Fatal("file is not downloadable")
			}
			if tc.name == "distribution" && !strings.Contains(w.Body.String(), "fake-key-0123456789") {
				t.Fatal("bait was not distributed")
			}
		})
	}
}

func testRuntime(t *testing.T, sink Sink) *Runtime {
	t.Helper()
	snapshot := testSnapshot()
	config, err := ParseConfig(t.Context(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return &Runtime{ctx: t.Context(), snapshot: snapshot, config: config, sink: sink}
}

func TestSinkFailureRejectsAccess(t *testing.T) {
	failure := errors.New("journal unavailable")
	r := testRuntime(t, func(context.Context, events.AgentEvent) error { return failure })
	w := httptest.NewRecorder()
	r.serveHTTP(w, httptest.NewRequest("GET", "/backup.txt", nil))
	if w.Code != 503 || !errors.Is(r.Err(), failure) || strings.Contains(w.Body.String(), "fake backup") {
		t.Fatalf("unsafe failure: %d %s %v", w.Code, w.Body.String(), r.Err())
	}
}
