package service

import (
	"context"
	"errors"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
)

func TestDeleteChecksBindingsAfterAccessAndRevision(t *testing.T) {
	for _, tc := range []struct {
		name                string
		role                string
		stale, absent, used bool
		cause, want         error
		calls               int
	}{
		{name: "unused profile", role: auth.RoleAdmin, calls: 1},
		{name: "bound profile", role: auth.RoleAdmin, used: true, want: profiles.ErrInUse, calls: 1},
		{name: "binding failure", role: auth.RoleAdmin, cause: context.DeadlineExceeded, want: context.DeadlineExceeded, calls: 1},
		{name: "missing binding check", role: auth.RoleAdmin, absent: true, want: profiles.ErrUnavailable},
		{name: "stale revision", role: auth.RoleAdmin, stale: true, used: true, want: profiles.ErrRevisionMismatch},
		{name: "viewer", role: auth.RoleViewer, used: true, want: profiles.ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store := testService()
			created, err := s.Create(t.Context(), admin, request())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			store.Bindings = func(ctx context.Context) (bool, error) {
				calls++
				if ctx != t.Context() {
					t.Fatal("binding check lost request context")
				}
				return tc.used, tc.cause
			}
			if tc.absent {
				store.Bindings = nil
			}
			actor := admin
			actor.Role = tc.role
			etag := profiles.ETag(created.Profile)
			if tc.stale {
				etag = profiles.ETag(profiles.Profile{ID: created.Profile.ID, Revision: 2})
			}
			err = s.Delete(t.Context(), actor, created.Profile.ID, etag)
			if !errors.Is(err, tc.want) || calls != tc.calls {
				t.Fatalf("delete: %v, calls=%d", err, calls)
			}
			if tc.want != nil {
				if store.Writes != 1 || len(store.Profiles) != 1 {
					t.Fatal("failed deletion changed state")
				}
			} else if store.Writes != 2 || len(store.Profiles) != 0 {
				t.Fatal("successful deletion missing")
			}
		})
	}
}
