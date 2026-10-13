//go:build integration

package repository

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	authcore "honey-forge/modules/auth"
	authservice "honey-forge/modules/auth/service"
)

func TestPostgresAPIKeyQuotaSharedAcrossServices(t *testing.T) {
	pool := integrationPool(t)
	repository := NewRepository(pool)
	first := authservice.NewService(repository).WithAPIKeyRateLimit(3)
	second := authservice.NewService(NewRepository(pool)).WithAPIKeyRateLimit(3)
	_, rawSession, err := first.Register(t.Context(), createRequest("concurrent-key@example.test"), "")
	if err != nil {
		t.Fatal(err)
	}

	session, err := first.ResolveSession(t.Context(), rawSession)
	if err != nil {
		t.Fatal(err)
	}

	key, raw, err := first.CreateAPIKey(t.Context(), session.AuthContext(), "Concurrent")
	if err != nil {
		t.Fatal(err)
	}

	var accepted, limited atomic.Int32
	var workers sync.WaitGroup
	for i := range 20 {
		service := first
		if i%2 == 0 {
			service = second
		}

		workers.Go(func() {
			_, err := service.AuthenticateAPIKey(t.Context(), raw)
			var rateError *authcore.APIKeyRateError
			if err == nil {
				accepted.Add(1)
			} else if errors.As(err, &rateError) && rateError.RetryAfter >= 1 && rateError.RetryAfter <= 60 {
				limited.Add(1)
			} else {
				t.Errorf("authentication: %v", err)
			}
		})
	}

	workers.Wait()
	if accepted.Load() != 3 || limited.Load() != 17 {
		t.Fatalf("accepted %d, limited %d", accepted.Load(), limited.Load())
	}

	if _, err := pool.Exec(t.Context(), `ALTER TABLE auth_audit ADD CONSTRAINT reject_key_revocation CHECK (action <> 'api_key.revoked') NOT VALID`); err != nil {
		t.Fatal(err)
	}

	if err := first.RevokeAPIKey(t.Context(), session.AuthContext(), key.ID); err == nil {
		t.Fatal("audit failure must roll back revocation")
	}

	if _, err := first.ResolveAPIKey(t.Context(), raw); err != nil {
		t.Fatalf("failed revocation removed key: %v", err)
	}
}
