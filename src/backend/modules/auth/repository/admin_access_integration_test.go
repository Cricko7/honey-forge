//go:build integration

package repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"honey-forge/modules/auth"
	authservice "honey-forge/modules/auth/service"
)

func TestPostgresAdminConfirmationExpiryAndAtomicAudit(t *testing.T) {
	pool := integrationPool(t)
	repo := NewRepository(pool)
	s := authservice.NewService(repo)
	_, raw, err := s.Register(t.Context(), createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.ResolveSession(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	actor := sess.AuthContext()
	if _, err := repo.Members(t.Context(), actor, 50, ""); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatalf("repository bypass: %v", err)
	}
	if err := repo.SetTrapPermission(t.Context(), actor, actor.UserID, false, 1); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatalf("repository mutation bypass: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `ALTER TABLE auth_audit ADD CONSTRAINT reject_admin_confirmation CHECK (action <> 'session.admin_confirmed') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), sess, "demo-password-2026"); err == nil {
		t.Fatal("audit failure accepted")
	}
	view, err := s.AdminAccess(t.Context(), actor)
	if err != nil || view.ExpiresAt != nil {
		t.Fatal("audit failure unlocked panel")
	}
	if _, err := pool.Exec(t.Context(), `ALTER TABLE auth_audit DROP CONSTRAINT reject_admin_confirmation`); err != nil {
		t.Fatal(err)
	}
	view, err = s.ConfirmAdminAccess(t.Context(), sess, "demo-password-2026")
	if err != nil || view.ExpiresAt == nil || time.Until(*view.ExpiresAt) > auth.AdminAccessTTL {
		t.Fatalf("confirmation=%+v err=%v", view, err)
	}
	if _, err := pool.Exec(t.Context(), `ALTER TABLE auth_audit ADD CONSTRAINT reject_admin_lock CHECK (action <> 'session.admin_locked') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if err := s.LockAdminAccess(t.Context(), actor); err == nil {
		t.Fatal("lock audit failure accepted")
	}
	stillConfirmed, err := s.AdminAccess(t.Context(), actor)
	if err != nil || stillConfirmed.ExpiresAt == nil {
		t.Fatal("failed lock changed confirmation")
	}
	if _, err := pool.Exec(t.Context(), `ALTER TABLE auth_audit DROP CONSTRAINT reject_admin_lock`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE operator_sessions SET admin_verified_until = now() - interval '1 second' WHERE id = $1`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Members(t.Context(), actor, 50, ""); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatalf("expired confirmation: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE operator_sessions SET expires_at = now() + interval '1 minute' WHERE id = $1`, sess.ID); err != nil {
		t.Fatal(err)
	}
	view, err = s.ConfirmAdminAccess(t.Context(), sess, "demo-password-2026")
	if err != nil || view.ExpiresAt == nil || time.Until(*view.ExpiresAt) > time.Minute {
		t.Fatal("confirmation extended session lifetime")
	}
	if err := s.LockAdminAccess(t.Context(), actor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Members(t.Context(), actor, 50, ""); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatal("lock did not protect members")
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), sess, "demo-password-2026"); err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(t.Context(), raw, sess.View.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminAccess(t.Context(), actor); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("revoked admin confirmation: %v", err)
	}
}

func TestPostgresAdminPasswordQuotaAcrossReplicasAndSessions(t *testing.T) {
	pool := integrationPool(t)
	repo := NewRepository(pool)
	s := authservice.NewService(repo)
	_, raw, err := s.Register(t.Context(), createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.ResolveSession(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Go(func() { results <- NewRepository(pool).ConsumeAdminAttempt(t.Context(), sess.AuthContext()) })
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		var rateError *auth.AdminPasswordRateError
		if err == nil {
			success++
		} else if !errors.As(err, &rateError) || rateError.RetryAfter < 1 {
			t.Fatal(err)
		}
	}
	if success != 5 {
		t.Fatalf("concurrent attempts admitted=%d", success)
	}
	_, otherRaw, err := s.Login(t.Context(), auth.LoginRequest{Email: sess.View.User.Email, Password: "demo-password-2026"}, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.ResolveSession(t.Context(), otherRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumeAdminAttempt(t.Context(), other.AuthContext()); err == nil {
		t.Fatal("new session bypassed quota")
	}
	if _, err := pool.Exec(t.Context(), `UPDATE users SET admin_auth_window = now() - interval '16 minutes' WHERE id = $1`, sess.View.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumeAdminAttempt(t.Context(), other.AuthContext()); err != nil {
		t.Fatal(err)
	}
	// Password changes between verification and commit cannot confirm a session.
	if _, err := repo.ConfirmAdminAccess(t.Context(), sess.AuthContext(), "stale-hash"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("stale hash accepted: %v", err)
	}
}
