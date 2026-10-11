package traplinks

import (
	"context"
	"errors"
	"testing"

	"honey-forge/internal/contract"
)

const orgID = "11111111-1111-4111-8111-111111111111"
const sourceID = "22222222-2222-4222-8222-222222222222"
const targetID = "33333333-3333-4333-8333-333333333333"

type fakeStore struct {
	calls int
	links []Link
}

func (f *fakeStore) List(context.Context, string) ([]Link, error) { f.calls++; return f.links, nil }
func (f *fakeStore) Create(_ context.Context, _ string, link Link) error {
	f.calls++
	f.links = append(f.links, link)
	return nil
}
func (f *fakeStore) Delete(context.Context, string, string, string) error { f.calls++; return nil }
func (f *fakeStore) Targets(context.Context, string, string) ([]string, error) {
	f.calls++
	return []string{targetID}, nil
}

func TestCreateAccessAndValidation(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		role                 contract.Role
		source, target, code string
	}{
		{"success", contract.Admin, sourceID, targetID, ""},
		{"viewer", contract.Viewer, sourceID, targetID, "forbidden"},
		{"agent", contract.Agent, sourceID, targetID, "forbidden"},
		{"anonymous", "", sourceID, targetID, "unauthenticated"},
		{"self link", contract.Admin, sourceID, sourceID, "validation_failed"},
		{"invalid target", contract.Admin, sourceID, "bad", "validation_failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeStore{}
			ctx := t.Context()
			if tt.role != "" {
				ctx = contract.WithPrincipal(ctx, contract.Principal{OrganizationID: orgID, TrapID: sourceID, Role: tt.role})
			}
			_, err := NewService(f).Create(ctx, Link{SourceID: tt.source, TargetID: tt.target})
			checkCode(t, err, tt.code)
			if tt.code != "" && f.calls != 0 {
				t.Fatal("storage called before authorization/validation")
			}
		})
	}
}

func TestTargetsBoundToAgentIdentity(t *testing.T) {
	f := &fakeStore{}
	ctx := contract.WithPrincipal(t.Context(), contract.Principal{OrganizationID: orgID, TrapID: sourceID, Role: contract.Agent})
	got, err := NewService(f).Targets(ctx, orgID, sourceID)
	checkCode(t, err, "")
	if len(got) != 1 || got[0] != targetID {
		t.Fatal(got)
	}
	_, err = NewService(f).Targets(ctx, orgID, targetID)
	checkCode(t, err, "resource_not_found")
	if f.calls != 1 {
		t.Fatal("foreign source reached storage")
	}
}

func checkCode(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var api *contract.Error
	if !errors.As(err, &api) || api.Code != want {
		t.Fatalf("error=%v, want=%s", err, want)
	}
}
