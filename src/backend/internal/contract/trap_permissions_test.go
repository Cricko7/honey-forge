package contract

import "testing"

func TestDelegatedTrapManagementDoesNotGrantAdministration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    Role
		granted bool
		want    bool
	}{
		{"admin", Admin, false, true},
		{"viewer", Viewer, false, false},
		{"delegate", Viewer, true, true},
		{"agent cannot delegate", Agent, true, false},
		{"unknown role", "unknown", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithPrincipal(t.Context(), Principal{Role: tc.role, CanManageTraps: tc.granted})
			if got := AuthorizeCapability(ctx, ManageTraps) == nil; got != tc.want {
				t.Fatalf("trap management=%v want %v", got, tc.want)
			}
			for _, cap := range []Capability{WriteResources, ManageCredentials} {
				if got := AuthorizeCapability(ctx, cap) == nil; got != (tc.role == Admin) {
					t.Fatalf("%s granted to %s", cap, tc.role)
				}
			}
		})
	}
}
