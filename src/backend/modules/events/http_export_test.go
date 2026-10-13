package events

import (
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

func TestExportHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, query  string
		role         contract.Role
		status       int
		storageError bool
	}{
		{"CSV viewer", "format=csv&type_id=tcp-banner", contract.Viewer, 200, false},
		{"STIX admin", "format=stix", contract.Admin, 200, false},
		{"missing format", "", contract.Viewer, 400, false},
		{"invalid format", "format=json", contract.Viewer, 400, false},
		{"invalid type", "format=csv&type_id=", contract.Viewer, 400, false},
		{"invalid trap", "format=csv&trap_id=no", contract.Viewer, 400, false},
		{"repeated parameter", "format=csv&format=stix", contract.Viewer, 400, false},
		{"pagination rejected", "format=csv&limit=1", contract.Viewer, 400, false},
		{"invalid date", "format=csv&from=tomorrow", contract.Viewer, 400, false},
		{"reversed dates", "format=csv&from=2026-10-13T00:00:00Z&to=2026-10-12T00:00:00Z", contract.Viewer, 422, false},
		{"unauthenticated", "format=csv", "", 401, false},
		{"agent forbidden", "format=csv", contract.Agent, 403, false},
		{"storage failure", "format=csv", contract.Viewer, 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &exportFake{}
			if tc.storageError {
				store.err = errors.New("private database failure")
			}

			h := &Handler{service: NewHistoryService(store), logger: slog.Default()}
			r := gin.New()
			r.Use(contract.Middleware(), func(c *gin.Context) {
				if tc.role != "" {
					contract.SetPrincipal(c, contract.Principal{Role: tc.role, OrganizationID: "org"})
				}
			})
			r.GET("/api/events/export", h.export)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/api/events/export?"+tc.query, nil))
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}

			if tc.status == 200 {
				if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing download headers")
				}

				if strings.Contains(tc.query, "type_id=") && store.queries[0].TypeID != "tcp-banner" {
					t.Fatal("type filter not forwarded")
				}
			} else {
				if w.Header().Get("Content-Disposition") != "" || strings.Contains(w.Body.String(), "private database") {
					t.Fatal("partial file or private failure exposed")
				}

				if !tc.storageError && len(store.queries) != 0 {
					t.Fatal("invalid or unauthorized request reached storage")
				}
			}
		})
	}
}
