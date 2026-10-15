//go:build integration

package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"honey-forge/modules/auth"
	authservice "honey-forge/modules/auth/service"
)

func TestPostgresMemberRemoval(t *testing.T) {
	pool := integrationPool(t)
	repo := NewRepository(pool)
	s := authservice.NewService(repo)
	ctx := t.Context()
	owner, raw, err := s.Register(ctx, createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.ResolveSession(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	actor := sess.AuthContext()
	code, err := s.JoinCode(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	join := createRequest("viewer@example.com")
	join.Organization = &auth.OrganizationInput{Mode: "join", JoinCode: code.Code}
	viewer, viewerRaw, err := s.Register(ctx, join, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveMember(ctx, actor, viewer.User.ID); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatalf("unconfirmed: %v", err)
	}
	if _, err := s.ConfirmAdminAccess(ctx, sess, "demo-password-2026"); err != nil {
		t.Fatal(err)
	}
	foreign, _, err := s.Register(ctx, createRequest("foreign@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	join.Email = "admin@example.com"
	admin, adminRaw, err := s.Register(ctx, join, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'admin' WHERE id = $1`, admin.User.ID); err != nil {
		t.Fatal(err)
	}
	adminSess, err := s.ResolveSession(ctx, adminRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdminAccess(ctx, adminSess, "demo-password-2026"); err != nil {
		t.Fatal(err)
	}
	forged := actor
	forged.OrganizationID = foreign.Organization.ID
	for _, tc := range []struct {
		name  string
		actor auth.AuthContext
		id    string
		want  error
	}{
		{"creator protected", actor, owner.User.ID, auth.ErrMemberProtected},
		{"other admin protected", actor, admin.User.ID, auth.ErrMemberProtected},
		{"noncreator admin denied", adminSess.AuthContext(), viewer.User.ID, auth.ErrForbidden},
		{"foreign target hidden", actor, foreign.User.ID, auth.ErrNotFound},
		{"foreign actor denied", forged, viewer.User.ID, auth.ErrForbidden},
		{"absent target", actor, auth.NewUUID(), auth.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := repo.RemoveMember(ctx, tc.actor, tc.id); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	if err := s.SetTrapPermission(ctx, actor, viewer.User.ID, true, 1); err != nil {
		t.Fatal(err)
	}
	_, otherRaw, err := s.Login(ctx, auth.LoginRequest{Email: viewer.User.Email, Password: "demo-password-2026"}, "")
	if err != nil {
		t.Fatal(err)
	}
	keyRaw := "hf_" + auth.NewOpaque(32)
	key := auth.APIKey{ID: auth.NewUUID(), UserID: viewer.User.ID, Name: "Historical key", Prefix: keyRaw[:11], Hash: auth.HashToken(keyRaw), CreatedAt: time.Now()}
	if err := repo.CreateAPIKey(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE auth_audit ADD CONSTRAINT reject_removal CHECK (action <> 'member.removed') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(ctx, actor, viewer.User.ID); err == nil {
		t.Fatal("audit failure accepted")
	}
	if _, err := s.ResolveSession(ctx, viewerRaw); err != nil {
		t.Fatalf("failed removal revoked session: %v", err)
	}
	if _, err := s.ResolveAPIKey(ctx, keyRaw); err != nil {
		t.Fatalf("failed removal revoked key: %v", err)
	}
	if user, _, _, err := repo.Credentials(ctx, viewer.User.Email); err != nil || !user.CanManageTraps {
		t.Fatalf("failed removal changed account: %+v %v", user, err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE auth_audit DROP CONSTRAINT reject_removal`); err != nil {
		t.Fatal(err)
	}
	// Simultaneous removal, browser login and key issuance must converge to no access.
	var wg sync.WaitGroup
	removals := make(chan error, 2)
	issuance := make(chan error, 8)
	start := make(chan struct{})
	for range 2 {
		wg.Go(func() { <-start; removals <- s.RemoveMember(ctx, actor, viewer.User.ID) })
	}
	for range 4 {
		wg.Go(func() {
			<-start
			newSession, _, _ := auth.NewSession(viewer.User.ID, time.Now())
			issuance <- repo.CreateSession(ctx, newSession, nil)
		})
		wg.Go(func() {
			<-start
			next := key
			next.ID = auth.NewUUID()
			next.Hash = auth.HashToken(auth.NewOpaque(32))
			issuance <- repo.CreateAPIKey(ctx, next)
		})
	}
	close(start)
	wg.Wait()
	close(removals)
	close(issuance)
	success, absent := 0, 0
	for err := range removals {
		if err == nil {
			success++
		} else if errors.Is(err, auth.ErrNotFound) {
			absent++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || absent != 1 {
		t.Fatalf("success=%d absent=%d", success, absent)
	}
	for err := range issuance {
		if err != nil && !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("concurrent issuance: %v", err)
		}
	}
	for _, cookie := range []string{viewerRaw, otherRaw} {
		if _, err := s.ResolveSession(ctx, cookie); !errors.Is(err, auth.ErrUnauthorized) {
			t.Fatalf("removed session: %v", err)
		}
	}
	if _, err := s.ResolveAPIKey(ctx, keyRaw); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("removed key: %v", err)
	}
	if _, _, err := s.Login(ctx, auth.LoginRequest{Email: viewer.User.Email, Password: "demo-password-2026"}, ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("removed login: %v", err)
	}
	join.Email = viewer.User.Email
	if _, _, err := s.Register(ctx, join, ""); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("removed email reused: %v", err)
	}
	if err := s.SetTrapPermission(ctx, actor, viewer.User.ID, true, 1); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("removed user permission: %v", err)
	}
	stale, _, _ := auth.NewSession(viewer.User.ID, time.Now())
	if err := repo.CreateSession(ctx, stale, nil); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("stale session issued: %v", err)
	}
	if err := repo.CreateAPIKey(ctx, key); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("stale key issued: %v", err)
	}
	page, err := s.Members(ctx, actor, 50, "")
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("members=%+v err=%v", page, err)
	}
	for _, member := range page.Items {
		if member.ID == viewer.User.ID {
			t.Fatal("removed member listed")
		}
	}
	var retained, removed, credentials, audits, history int
	err = pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM users WHERE id = $1),
		(SELECT count(*) FROM users WHERE id = $1 AND removed_at IS NOT NULL AND NOT trap_management),
		(SELECT count(*) FROM operator_sessions WHERE user_id = $1) + (SELECT count(*) FROM operator_api_keys WHERE user_id = $1),
		(SELECT count(*) FROM auth_audit WHERE action = 'member.removed' AND resource_id = $1),
		(SELECT count(*) FROM auth_audit WHERE actor_user_id = $1 AND action = 'organization.viewer_joined')`, viewer.User.ID).
		Scan(&retained, &removed, &credentials, &audits, &history)
	if err != nil {
		t.Fatal(err)
	}
	if retained != 1 || removed != 1 || credentials != 0 || audits != 1 || history != 1 {
		t.Fatalf("retained=%d removed=%d credentials=%d audits=%d history=%d", retained, removed, credentials, audits, history)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET trap_management = true WHERE id = $1`, viewer.User.ID); err == nil {
		t.Fatal("removed user regained permission")
	}
	if _, err := s.ResolveSession(ctx, raw); err != nil {
		t.Fatalf("owner session revoked: %v", err)
	}
	// A schema rollback must never silently reactivate retained accounts.
	data, err := os.ReadFile(filepath.Join("../../../../../migrations", "20261015000100_member_removal.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, strings.Split(string(data), "-- +goose Down")[1]); err == nil {
		t.Fatal("unsafe rollback accepted")
	}
	if _, _, err := s.Login(ctx, auth.LoginRequest{Email: viewer.User.Email, Password: "demo-password-2026"}, ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("rollback restored access: %v", err)
	}
}
