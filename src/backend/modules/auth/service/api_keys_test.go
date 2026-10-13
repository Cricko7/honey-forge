package service

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	authcore "honey-forge/modules/auth"
	"honey-forge/modules/auth/internal/teststore"
)

func TestAPIKeyAuthorizationAndQuota(t *testing.T) {
	repo := teststore.New()
	service := NewService(repo).WithAPIKeyRateLimit(2)
	_, cookie, err := service.Register(t.Context(), createRequest("api-key@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}

	session, err := service.ResolveSession(t.Context(), cookie)
	if err != nil {
		t.Fatal(err)
	}

	actor := session.AuthContext()
	viewer := actor
	viewer.Role = authcore.RoleViewer
	if _, _, err := service.CreateAPIKey(t.Context(), viewer, "Forbidden"); !errors.Is(err, authcore.ErrForbidden) {
		t.Fatalf("viewer creation: %v", err)
	}

	if _, err := service.ListAPIKeys(t.Context(), viewer); !errors.Is(err, authcore.ErrForbidden) {
		t.Fatalf("viewer list: %v", err)
	}

	if err := service.RevokeAPIKey(t.Context(), viewer, authcore.NewUUID()); !errors.Is(err, authcore.ErrForbidden) {
		t.Fatalf("viewer revoke: %v", err)
	}

	key, raw, err := service.CreateAPIKey(t.Context(), actor, "Integration")
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(repo.Keys[key.ID].Hash, []byte(raw)) || !bytes.Equal(repo.Keys[key.ID].Hash, authcore.HashToken(raw)) {
		t.Fatal("key must be stored as a hash")
	}

	for range 10 {
		if _, err := service.ResolveAPIKey(t.Context(), raw); err != nil {
			t.Fatal(err)
		}
	}

	var accepted atomic.Int32
	var limited atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_, err := service.AuthenticateAPIKey(t.Context(), raw)
			var rateError *authcore.APIKeyRateError
			if err == nil {
				accepted.Add(1)
			} else if errors.As(err, &rateError) && rateError.RetryAfter > 0 {
				limited.Add(1)
			} else {
				t.Errorf("authentication: %v", err)
			}
		})
	}

	wg.Wait()
	if accepted.Load() != 2 || limited.Load() != 8 {
		t.Fatalf("accepted %d, limited %d", accepted.Load(), limited.Load())
	}

	other := actor
	other.OrganizationID = authcore.NewUUID()
	if err := service.RevokeAPIKey(t.Context(), other, key.ID); !errors.Is(err, authcore.ErrNotFound) {
		t.Fatalf("cross-organization revoke: %v", err)
	}

	if err := service.RevokeAPIKey(t.Context(), actor, key.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.ResolveAPIKey(t.Context(), raw); !errors.Is(err, authcore.ErrUnauthorized) {
		t.Fatalf("revoked key: %v", err)
	}
}
