package service

import (
	"errors"
	profilecore "honey-forge/src/backend/modules/profiles"
	"testing"
)

func TestServicePaginationIsolationAndBoundary(t *testing.T) {
	s, store := testService()
	for range 4 {
		req := request()
		req.RequestID = profilecore.NewID()
		if _, e := s.Create(t.Context(), admin, req); e != nil {
			t.Fatal(e)
		}
	}

	page, e := s.List(t.Context(), admin, profilecore.ListOptions{Limit: 2, TypeID: "demo"})
	if e != nil || len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatalf("first: %#v %v", page, e)
	}

	firstToken := *page.NextCursor
	req := request()
	req.RequestID = profilecore.NewID()
	if _, e := s.Create(t.Context(), admin, req); e != nil {
		t.Fatal(e)
	}

	second, e := s.List(t.Context(), admin, profilecore.ListOptions{Limit: 2, TypeID: "demo", Cursor: firstToken})
	if e != nil || len(second.Items) != 2 || second.NextCursor != nil {
		t.Fatalf("second: %#v %v", second, e)
	}

	seen := map[string]bool{}
	for _, p := range append(page.Items, second.Items...) {
		if seen[p.ID] {
			t.Fatal("overlapping pages")
		}

		seen[p.ID] = true
		if p.Sequence > 4 {
			t.Fatal("new profile entered old page")
		}
	}

	foreign := admin
	foreign.OrganizationID = requestID
	for _, tc := range []struct {
		name    string
		opts    profilecore.ListOptions
		foreign bool
	}{
		{"foreign organization", profilecore.ListOptions{TypeID: "demo", Cursor: firstToken}, true},
		{"other filter", profilecore.ListOptions{Cursor: firstToken}, false},
		{"tampering", profilecore.ListOptions{TypeID: "demo", Cursor: firstToken + "x"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := admin
			if tc.foreign {
				a = foreign
			}

			if _, e := s.List(t.Context(), a, tc.opts); !errors.Is(e, profilecore.ErrInvalidCursor) {
				t.Fatalf("error: %v", e)
			}
		})
	}

	empty, e := s.List(t.Context(), admin, profilecore.ListOptions{TypeID: "unknown"})
	if e != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatalf("empty: %#v %v", empty, e)
	}

	if store.writes != 5 {
		t.Fatal("reads mutated state")
	}
}
