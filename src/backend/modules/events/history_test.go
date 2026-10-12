package events

import (
	"context"
	"errors"
	"testing"

	"honey-forge/internal/contract"
)

type historyFake struct {
	calls int
	ctx   context.Context
	query Query
	id    string
	err   error
}

func (f *historyFake) Read(ctx context.Context, id string) (Event, error) {
	f.calls++
	f.ctx, f.id = ctx, id

	return Event{AgentEvent: AgentEvent{EventID: id}}, f.err
}

func (f *historyFake) List(ctx context.Context, q Query) ([]EventSummary, bool, int64, int64, error) {
	f.calls++
	f.ctx, f.query = ctx, q

	return []EventSummary{{EventID: "event"}}, true, 5, 7, f.err
}

func TestHistoryReadAndList(t *testing.T) {
	for _, role := range []contract.Role{contract.Admin, contract.Viewer} {
		t.Run(string(role), func(t *testing.T) {
			ctx := contract.WithPrincipal(t.Context(), contract.Principal{Role: role, OrganizationID: "org"})
			store := &historyFake{}
			s := NewHistoryService(store)
			event, err := s.Read(ctx, "id")
			if err != nil || event.EventID != "id" || store.ctx != ctx || store.id != "id" {
				t.Fatalf("read: %+v %v", event, err)
			}

			q := Query{Limit: 10, TrapID: "trap", Boundary: 5}
			items, more, boundary, sequence, err := s.List(ctx, q)
			if err != nil || len(items) != 1 || !more || boundary != 5 || sequence != 7 || store.ctx != ctx || store.query != q {
				t.Fatalf("list: %+v %v %d %d %v", items, more, boundary, sequence, err)
			}

			store.err = errors.New("storage failed")
			if _, err := s.Read(ctx, "id"); !errors.Is(err, store.err) {
				t.Fatal("read lost storage cause")
			}

			if _, _, _, _, err := s.List(ctx, q); !errors.Is(err, store.err) {
				t.Fatal("list lost storage cause")
			}
		})
	}
}

func TestHistoryRejectsBeforeStorage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		role   contract.Role
		cancel bool
		code   string
	}{
		{"unauthenticated", "", false, "unauthenticated"},
		{"agent", contract.Agent, false, "forbidden"},
		{"canceled viewer", contract.Viewer, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tc.role != "" {
				ctx = contract.WithPrincipal(ctx, contract.Principal{Role: tc.role})
			}

			if tc.cancel {
				cancel()
			}

			store := &historyFake{}
			s := NewHistoryService(store)
			_, readErr := s.Read(ctx, "id")
			_, _, _, _, listErr := s.List(ctx, Query{})
			for _, err := range []error{readErr, listErr} {
				if tc.cancel {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error: %v", err)
					}
				} else {
					var api *contract.Error
					if !errors.As(err, &api) || api.Code != tc.code {
						t.Fatalf("error: %v", err)
					}
				}
			}

			if store.calls != 0 {
				t.Fatal("unauthorized or canceled storage read")
			}
		})
	}
}
