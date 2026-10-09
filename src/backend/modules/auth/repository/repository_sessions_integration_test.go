//go:build integration

package repository

import (
	"errors"
	"math"
	"testing"
)

func TestPostgresFailedLoginReplacementPreservesSession(t *testing.T) {
	pool := integrationPool(t)
	service := NewService(NewRepository(pool))
	ctx := t.Context()
	_, raw, err := service.Register(ctx, createRequest("admin@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, "ALTER TABLE auth_audit ADD CONSTRAINT reject_created_session CHECK (action <> 'session.created') NOT VALID")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Login(ctx, LoginRequest{"admin@example.com", "demo-password-2026"}, raw); err == nil {
		t.Fatal("audit failure must fail login replacement")
	}
	if _, err := service.ResolveSession(ctx, raw); err != nil {
		t.Fatal("failed replacement revoked the old session")
	}
	if sqlCount(t, pool, "operator_sessions") != 1 || sqlCount(t, pool, "auth_audit") != 2 || sqlCount(t, pool, "auth_changes") != 2 {
		t.Fatal("failed replacement left partial session/audit state")
	}
}

func TestPostgresRevisionExhaustionAndSessionExpiry(t *testing.T) {
	pool := integrationPool(t)
	service := NewService(NewRepository(pool))
	ctx := t.Context()
	view, raw, err := service.Register(ctx, createRequest("admin@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := service.ResolveSession(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, "UPDATE organizations SET join_code_revision = $1 WHERE id = $2", int32(math.MaxInt32), view.Organization.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := service.JoinCode(ctx, sess.AuthContext())
	if _, err := service.RotateJoinCode(ctx, sess.AuthContext(), math.MaxInt32); !errors.Is(err, ErrRevisionExhausted) {
		t.Fatalf("revision exhaustion: %v", err)
	}
	after, _ := service.JoinCode(ctx, sess.AuthContext())
	if before != after {
		t.Fatal("exhausted revision changed join code")
	}

	_, err = pool.Exec(ctx, "UPDATE operator_sessions SET expires_at = now() - interval '1 second'")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveSession(ctx, raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("expired PostgreSQL session authenticated")
	}
	if err := service.Logout(ctx, raw, ""); err != nil {
		t.Fatal("expired session logout should be idempotent without CSRF")
	}
}
