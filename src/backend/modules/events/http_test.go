package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
)

type pageFake struct{ calls int }

func (f *pageFake) Read(context.Context, string) (Event, error) { return Event{}, nil }

func (f *pageFake) List(context.Context, Query) ([]EventSummary, bool, int64, int64, error) {
	f.calls++
	return []EventSummary{{EventID: "22222222-2222-4222-8222-222222222222", OccurredAt: "2026-10-12T07:30:00Z"}}, f.calls == 1, 1, 1, nil
}

func TestEventCursorNormalizesTimeFilters(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		status         int
	}{
		{"same spelling", "2026-10-12T10:00:00+03:00", "2026-10-12T11:00:00+03:00", 200},
		{"same instants in UTC", "2026-10-12T07:00:00Z", "2026-10-12T08:00:00Z", 200},
		{"different instant", "2026-10-12T07:01:00Z", "2026-10-12T08:00:00Z", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			codec, err := contract.NewCursorCodec(make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			store := &pageFake{}
			h := &Handler{service: NewHistoryService(store), cursors: codec, logger: slog.Default()}
			r := gin.New()
			r.Use(contract.Middleware(), func(c *gin.Context) {
				contract.SetPrincipal(c, contract.Principal{Role: contract.Viewer, OrganizationID: "11111111-1111-4111-8111-111111111111"})
			})
			r.GET("/api/events", h.list)
			query := url.Values{"from": {"2026-10-12T10:00:00+03:00"}, "to": {"2026-10-12T11:00:00+03:00"}, "limit": {"1"}}
			first := httptest.NewRecorder()
			r.ServeHTTP(first, httptest.NewRequest("GET", "/api/events?"+query.Encode(), nil))
			var page EventPage
			if first.Code != 200 || json.Unmarshal(first.Body.Bytes(), &page) != nil || page.NextCursor == nil {
				t.Fatalf("first page: %d %s", first.Code, first.Body.String())
			}

			query.Set("from", tc.from)
			query.Set("to", tc.to)
			query.Set("cursor", *page.NextCursor)
			second := httptest.NewRecorder()
			r.ServeHTTP(second, httptest.NewRequest("GET", "/api/events?"+query.Encode(), nil))
			if second.Code != tc.status {
				t.Fatalf("second page: %d %s, want %d", second.Code, second.Body.String(), tc.status)
			}
			if tc.status == 400 && store.calls != 1 {
				t.Fatal("changed filters reached storage")
			}
		})
	}
}
