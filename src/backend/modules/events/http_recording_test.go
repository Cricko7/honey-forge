package events

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"honey-forge/internal/contract"
)

func TestRecordingHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		role       contract.Role
		status     int
	}{
		{"success", "/api/events/recordings/22222222-2222-4222-8222-222222222222?trap_id=11111111-1111-4111-8111-111111111111", contract.Viewer, 200},
		{"missing trap", "/api/events/recordings/22222222-2222-4222-8222-222222222222", contract.Viewer, 400},
		{"invalid session", "/api/events/recordings/invalid?trap_id=11111111-1111-4111-8111-111111111111", contract.Viewer, 400},
		{"unauthenticated", "/api/events/recordings/22222222-2222-4222-8222-222222222222?trap_id=11111111-1111-4111-8111-111111111111", "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingFake{events: []Event{{AgentEvent: AgentEvent{EventType: "service.connection_opened", SessionSequence: 1}}}}
			h := &Handler{recordings: NewRecordingService(store), logger: slog.Default()}
			r := gin.New()
			r.Use(contract.Middleware(), func(c *gin.Context) {
				if tc.role != "" {
					contract.SetPrincipal(c, contract.Principal{Role: tc.role, OrganizationID: "11111111-1111-4111-8111-111111111111"})
				}
			})
			r.GET("/api/events/recordings/:session_id", h.recording)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
