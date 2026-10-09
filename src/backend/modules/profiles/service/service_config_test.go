package service

import (
	"context"
	"encoding/json"
	"errors"
	profilecore "honey-forge/src/backend/modules/profiles"
	"strings"
	"testing"
)

func TestServiceCatalogAndEffectiveConfig(t *testing.T) {
	s, store := testService()
	req := request()
	req.Config["remove_me"] = "old"
	created, e := s.Create(t.Context(), admin, req)
	if e != nil {
		t.Fatal(e)
	}

	lookup := s.deps.LookupType
	s.deps.LookupType = func(ctx context.Context, id string, version int32) (profilecore.Type, error) {
		typ, e := lookup(ctx, id, version)
		typ.AvailableForNewProfiles = false
		return typ, e
	}

	if _, e := s.Create(t.Context(), admin, req); e != nil {
		t.Fatal("deprecated replay rejected", e)
	}

	other := request()
	other.RequestID = userID
	if _, e := s.Create(t.Context(), admin, other); !errors.Is(e, profilecore.ErrTypeUnavailable) {
		t.Fatalf("deprecated create: %v", e)
	}

	p, e := s.Patch(t.Context(), admin, created.Profile.ID, profilecore.ETag(created.Profile), profilecore.PatchRequest{Config: profilecore.Object{"visible": "two"}})
	if e != nil {
		t.Fatal("deprecated patch rejected", e)
	}

	snap, e := s.ReadSnapshot(t.Context(), orgID, p.ID, 2)
	if e != nil {
		t.Fatal(e)
	}

	if _, exists := snap.Config["remove_me"]; exists {
		t.Fatal("config recursively merged")
	}

	s.deps.LookupType = func(ctx context.Context, id string, version int32) (profilecore.Type, error) {
		typ, e := lookup(ctx, id, version)
		typ.CheckConfig = func(ctx context.Context, c profilecore.Object) error {
			if _, exists := profilecore.PointerGet(c, "/credentials/password"); !exists {
				return profilecore.ErrConfigInvalid
			}

			return ctx.Err()
		}

		return typ, e
	}

	if _, e := s.Patch(t.Context(), admin, p.ID, profilecore.ETag(p), profilecore.PatchRequest{
		ClearSecretFields: []string{"/credentials/password"},
	}); !errors.Is(e, profilecore.ErrConfigInvalid) {
		t.Fatalf("required secret cleared: %v", e)
	}

	if store.writes != 2 {
		t.Fatal("failed mutation wrote state")
	}

	s, _ = testService()
	req = request()
	req.Config["credentials"] = profilecore.Object{"password": strings.Repeat("x", 65000)}
	created, e = s.Create(t.Context(), admin, req)
	if e != nil {
		t.Fatal(e)
	}

	if _, e := s.Patch(t.Context(), admin, created.Profile.ID, profilecore.ETag(created.Profile), profilecore.PatchRequest{
		Config: profilecore.Object{"visible": strings.Repeat("x", 70000)},
	}); !errors.Is(e, profilecore.ErrConfigTooLarge) {
		t.Fatalf("effective config overflow: %v", e)
	}
}

func TestServiceNormalizationAndCancellation(t *testing.T) {
	s, store := testService()
	req := request()
	req.Config["number"] = json.Number("9007199254740993")
	created, e := s.Create(t.Context(), admin, req)
	if e != nil {
		t.Fatal(e)
	}

	req.Name = "Demo"
	req.Config["number"] = json.Number("9007199254740993.0")
	if r, e := s.Create(t.Context(), admin, req); e != nil || !r.Replayed {
		t.Fatalf("normalization: %#v %v", r, e)
	}

	req.Config["number"] = json.Number("9007199254740992")
	if _, e := s.Create(t.Context(), admin, req); !errors.Is(e, profilecore.ErrIdempotencyConflict) {
		t.Fatalf("rounded hash: %v", e)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"create", func() error { _, e := s.Create(ctx, admin, request()); return e }},
		{"read", func() error { _, e := s.ReadProfile(ctx, admin, created.Profile.ID); return e }},
		{"patch", func() error {
			_, e := s.Patch(ctx, admin, created.Profile.ID, profilecore.ETag(created.Profile), profilecore.PatchRequest{Name: ptr("X")})
			return e
		}},
		{"delete", func() error { return s.Delete(ctx, admin, created.Profile.ID, profilecore.ETag(created.Profile)) }},
		{"list", func() error { _, e := s.List(ctx, admin, profilecore.ListOptions{}); return e }},
		{"snapshot", func() error { _, e := s.ReadSnapshot(ctx, orgID, created.Profile.ID, 1); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.call(); !errors.Is(e, context.Canceled) {
				t.Fatalf("cancellation: %v", e)
			}
		})
	}

	if store.writes != 1 {
		t.Fatal("cancelled request wrote state")
	}
}

func TestServicePreservesSecretsInOmittedArray(t *testing.T) {
	s, _ := testService()
	s.deps.LookupType = func(context.Context, string, int32) (profilecore.Type, error) {
		return profilecore.Type{
			InteractionLevel: "low", AvailableForNewProfiles: true,
			SecretPaths:  func(profilecore.Object) []string { return []string{"/servers/0/password"} },
			IsSecretPath: func(path string) bool { return path == "/servers/0/password" },
			CheckConfig: func(ctx context.Context, c profilecore.Object) error {
				servers, ok := c["servers"].([]any)
				if !ok || len(servers) != 1 {
					return profilecore.ErrConfigInvalid
				}

				return ctx.Err()
			},
		}, nil
	}

	req := request()
	req.Config = profilecore.Object{"visible": "one", "servers": []any{profilecore.Object{"password": "hidden"}}}
	created, e := s.Create(t.Context(), admin, req)
	if e != nil {
		t.Fatal(e)
	}

	p, e := s.Patch(t.Context(), admin, created.Profile.ID, profilecore.ETag(created.Profile), profilecore.PatchRequest{Config: profilecore.Object{"visible": "two"}})
	if e != nil {
		t.Fatalf("omitted secret array: %v", e)
	}

	snap, e := s.ReadSnapshot(t.Context(), orgID, p.ID, 2)
	if e != nil {
		t.Fatal(e)
	}

	if !strings.Contains(mustJSON(snap.Config), "hidden") || strings.Contains(mustJSON(p.Config), "hidden") {
		t.Fatal("wrong array secret preservation/redaction")
	}
}
