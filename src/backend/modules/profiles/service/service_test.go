package service

import (
	"context"
	"errors"
	profilecore "honey-forge/src/backend/modules/profiles"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"honey-forge/src/backend/modules/auth"
)

const (
	orgID     = "11111111-1111-4111-8111-111111111111"
	userID    = "22222222-2222-4222-8222-222222222222"
	requestID = "33333333-3333-4333-8333-333333333333"
)

var admin = auth.AuthContext{UserID: userID, OrganizationID: orgID, Role: auth.RoleAdmin}

func request() profilecore.CreateRequest {
	return profilecore.CreateRequest{
		RequestID:   requestID,
		Name:        " Demo ",
		TypeID:      "demo",
		TypeVersion: 1,
		Config: profilecore.Object{
			"visible":     "one",
			"credentials": profilecore.Object{"password": "private"},
		},
	}
}

func testService() (*Service, *fakeStore) {
	store := &fakeStore{profiles: map[string]profilecore.Profile{}, keys: map[string]fakeKey{}, snapshots: map[string]profilecore.Profile{}}
	deps := profilecore.Dependencies{
		LookupType: func(ctx context.Context, id string, version int32) (profilecore.Type, error) {
			if err := ctx.Err(); err != nil {
				return profilecore.Type{}, err
			}

			if id != "demo" || version != 1 {
				return profilecore.Type{}, profilecore.ErrUnknownType
			}

			return profilecore.Type{InteractionLevel: "low", AvailableForNewProfiles: true,
				SecretPaths:  func(profilecore.Object) []string { return []string{"/credentials/password"} },
				IsSecretPath: func(p string) bool { return p == "/credentials/password" },
				CheckConfig: func(ctx context.Context, c profilecore.Object) error {
					if err := ctx.Err(); err != nil {
						return err
					}

					if _, ok := c["visible"].(string); !ok {
						return profilecore.ErrConfigInvalid
					}

					return nil
				},
			}, nil
		},
		HasLiveBindings: func(ctx context.Context, org, id string, tx pgx.Tx) (bool, error) { return false, ctx.Err() },
	}

	return NewService(store, deps), store
}

func TestServiceCreate(t *testing.T) {
	s, store := testService()
	created, err := s.Create(t.Context(), admin, request())
	if err != nil {
		t.Fatal(err)
	}

	p := created.Profile
	if p.Name != "Demo" || p.Revision != 1 || !created.Current || created.Replayed || len(p.SecretFieldsSet) != 1 {
		t.Fatalf("created = %#v", created)
	}

	if strings.Contains(mustJSON(p), "private") {
		t.Fatal("secret leaked")
	}

	if store.writes != 1 {
		t.Fatal("missing mutation")
	}

	patch := profilecore.PatchRequest{Name: ptr("Changed")}
	updated, err := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(p), patch)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("patch: %#v %v", updated, err)
	}

	replay, err := s.Create(t.Context(), admin, request())
	if err != nil || !replay.Replayed || replay.Current || replay.Profile.Name != "Demo" ||
		replay.Profile.Revision != 1 || store.writes != 2 {
		t.Fatalf("replay: %#v %v", replay, err)
	}

	req := request()
	req.Description = "different"
	if _, err := s.Create(t.Context(), admin, req); !errors.Is(err, profilecore.ErrIdempotencyConflict) {
		t.Fatalf("conflict = %v", err)
	}

	if err := s.Delete(t.Context(), admin, p.ID, profilecore.ETag(updated)); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Create(t.Context(), admin, request()); !errors.Is(err, profilecore.ErrRequestUsed) {
		t.Fatalf("deleted replay = %v", err)
	}

	if _, err := s.ReadSnapshot(t.Context(), orgID, p.ID, 1); err != nil {
		t.Fatal("deleted profile lost snapshot", err)
	}
}

func TestServicePatchSecretsAndNoop(t *testing.T) {
	s, store := testService()
	created, err := s.Create(t.Context(), admin, request())
	if err != nil {
		t.Fatal(err)
	}

	p := created.Profile
	updated, err := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(p), profilecore.PatchRequest{Config: profilecore.Object{"visible": "two"}})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("patch: %#v %v", updated, err)
	}

	snap, err := s.ReadSnapshot(t.Context(), orgID, p.ID, 2)
	if err != nil || !strings.Contains(mustJSON(snap.Config), "private") {
		t.Fatal("secret was not preserved", err)
	}

	old, err := s.ReadSnapshot(t.Context(), orgID, p.ID, 1)
	if err != nil || old.Config["visible"] != "one" {
		t.Fatal("old snapshot mutated", err)
	}

	noop, err := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(updated), profilecore.PatchRequest{Config: profilecore.Object{"visible": "two"}})
	if err != nil || noop.Revision != 2 || store.writes != 2 {
		t.Fatalf("noop: %#v %v", noop, err)
	}

	cleared, err := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(noop), profilecore.PatchRequest{
		ClearSecretFields: []string{"/credentials/password"},
	})
	if err != nil || len(cleared.SecretFieldsSet) != 0 {
		t.Fatalf("clear: %#v %v", cleared, err)
	}
}

func TestServiceFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*Service, profilecore.Profile) error
		want error
	}{
		{"viewer write", func(s *Service, p profilecore.Profile) error {
			a := admin
			a.Role = auth.RoleViewer
			_, e := s.Patch(t.Context(), a, p.ID, profilecore.ETag(p), profilecore.PatchRequest{Name: ptr("X")})
			return e
		}, profilecore.ErrForbidden},
		{"unauthenticated", func(s *Service, p profilecore.Profile) error {
			_, e := s.ReadProfile(t.Context(), auth.AuthContext{}, p.ID)
			return e
		}, profilecore.ErrUnauthorized},
		{"foreign organization", func(s *Service, p profilecore.Profile) error {
			a := admin
			a.OrganizationID = requestID
			_, e := s.ReadProfile(t.Context(), a, p.ID)
			return e
		}, profilecore.ErrNotFound},
		{"stale etag", func(s *Service, p profilecore.Profile) error {
			_, e := s.Patch(t.Context(), admin, p.ID, `"profile:`+p.ID+`:2"`, profilecore.PatchRequest{Name: ptr("X")})
			return e
		}, profilecore.ErrRevisionMismatch},
		{"missing etag", func(s *Service, p profilecore.Profile) error {
			return s.Delete(t.Context(), admin, p.ID, "")
		}, profilecore.ErrPreconditionRequired},
		{"wildcard etag", func(s *Service, p profilecore.Profile) error {
			return s.Delete(t.Context(), admin, p.ID, "*")
		}, profilecore.ErrInvalidPrecondition},
		{"empty patch", func(s *Service, p profilecore.Profile) error {
			_, e := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(p), profilecore.PatchRequest{})
			return e
		}, profilecore.ErrValidation},
		{"bad config", func(s *Service, p profilecore.Profile) error {
			_, e := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(p), profilecore.PatchRequest{Config: profilecore.Object{}})
			return e
		}, profilecore.ErrConfigInvalid},
		{"bad clear", func(s *Service, p profilecore.Profile) error {
			_, e := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(p), profilecore.PatchRequest{ClearSecretFields: []string{"/visible"}})
			return e
		}, profilecore.ErrValidation},
		{"clear and set", func(s *Service, p profilecore.Profile) error {
			_, e := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(p), profilecore.PatchRequest{
				Config: request().Config,
				ClearSecretFields: []string{
					"/credentials/password",
				},
			})
			return e
		}, profilecore.ErrValidation},
		{"config size", func(s *Service, p profilecore.Profile) error {
			req := request()
			req.RequestID = userID
			req.Config["visible"] = strings.Repeat("я", 128<<10)
			_, e := s.Create(t.Context(), admin, req)
			return e
		}, profilecore.ErrConfigTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store := testService()
			c, e := s.Create(t.Context(), admin, request())
			if e != nil {
				t.Fatal(e)
			}

			if e := tc.run(s, c.Profile); !errors.Is(e, tc.want) {
				t.Fatalf("error=%v want=%v", e, tc.want)
			}

			if store.writes != 1 {
				t.Fatal("failed request changed state")
			}
		})
	}
}

func TestServiceDependenciesAndConcurrency(t *testing.T) {
	s, store := testService()
	c, e := s.Create(t.Context(), admin, request())
	if e != nil {
		t.Fatal(e)
	}

	s.deps.HasLiveBindings = func(context.Context, string, string, pgx.Tx) (bool, error) { return true, nil }
	if e := s.Delete(t.Context(), admin, c.Profile.ID, profilecore.ETag(c.Profile)); !errors.Is(e, profilecore.ErrInUse) {
		t.Fatalf("binding=%v", e)
	}

	s.deps.HasLiveBindings = func(context.Context, string, string, pgx.Tx) (bool, error) {
		return false, errors.New("bindings offline")
	}

	if e := s.Delete(t.Context(), admin, c.Profile.ID, profilecore.ETag(c.Profile)); e == nil {
		t.Fatal("dependency failure accepted")
	}

	store.profiles[c.Profile.ID] = func() profilecore.Profile { p := store.profiles[c.Profile.ID]; p.Revision = math.MaxInt32; return p }()
	if _, e := s.Patch(t.Context(), admin, c.Profile.ID,
		profilecore.ETag(store.profiles[c.Profile.ID]), profilecore.PatchRequest{Name: ptr("X")}); !errors.Is(e, profilecore.ErrRevisionExhausted) {
		t.Fatalf("overflow=%v", e)
	}

	s, store = testService()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, e := s.Create(t.Context(), admin, request()); errs <- e })
	}

	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}

	if len(store.profiles) != 1 || store.writes != 1 {
		t.Fatal("duplicate concurrent creates")
	}
}

func ptr(s string) *string { return &s }

func TestServiceConfigNullParentAndReplacement(t *testing.T) {
	s, _ := testService()
	created, e := s.Create(t.Context(), admin, request())
	if e != nil {
		t.Fatal(e)
	}

	if _, e := s.Patch(t.Context(), admin, created.Profile.ID, profilecore.ETag(created.Profile), profilecore.PatchRequest{
		Config: profilecore.Object{"visible": "two", "credentials": nil},
	}); !errors.Is(e, profilecore.ErrConfigInvalid) {
		t.Fatalf("null parent silently replaced: %v", e)
	}
}
