package audit

import (
	"context"
	"errors"
	"honey-forge/internal/contract"
	"testing"
)

type fakeStore struct {
	calls int
	err   error
}

func (f *fakeStore) Read(context.Context, string, string) (Entry, error) {
	f.calls++
	return Entry{Action: "trap.deleted"}, f.err
}
func (f *fakeStore) List(context.Context, string, Query) ([]Entry, bool, int64, error) {
	f.calls++
	return []Entry{}, false, 0, f.err
}

func TestReadAuthorizationAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		role contract.Role
		fail bool
		code string
	}{
		{"admin", contract.Admin, false, ""}, {"viewer", contract.Viewer, false, ""},
		{"anonymous", "", false, "unauthenticated"}, {"agent", contract.Agent, false, "forbidden"},
		{"missing", contract.Viewer, true, "resource_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeStore{}
			if tc.fail {
				f.err = contract.NewError("resource_not_found")
			}
			ctx := t.Context()
			if tc.role != "" {
				ctx = contract.WithPrincipal(ctx, contract.Principal{Role: tc.role, OrganizationID: contract.NewID()})
			}
			got, err := NewService(f).Read(ctx, string(contract.NewID()))
			if tc.code == "" {
				if err != nil || got.Action != "trap.deleted" {
					t.Fatalf("%+v %v", got, err)
				}
				return
			}
			var api *contract.Error
			if !errors.As(err, &api) || api.Code != tc.code {
				t.Fatalf("%v", err)
			}
			if (tc.role == "" || tc.role == contract.Agent) && f.calls != 0 {
				t.Fatal("unauthorized store call")
			}
		})
	}
}

func TestCancelledReadDoesNotReachStore(t *testing.T) {
	ctx, cancel := context.WithCancel(contract.WithPrincipal(t.Context(), contract.Principal{Role: contract.Admin, OrganizationID: contract.NewID()}))
	cancel()
	f := &fakeStore{}
	_, err := NewService(f).Read(ctx, string(contract.NewID()))
	if !errors.Is(err, context.Canceled) || f.calls != 0 {
		t.Fatalf("%v calls=%d", err, f.calls)
	}
}
