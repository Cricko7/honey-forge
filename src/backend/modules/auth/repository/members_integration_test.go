//go:build integration

package repository

import (
	"errors"
	"sync"
	"testing"

	"honey-forge/modules/auth"
	authservice "honey-forge/modules/auth/service"
)

func TestPostgresMemberPermissionsAndPresence(t *testing.T) {
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
	if _, err := s.ConfirmAdminAccess(ctx, sess, "demo-password-2026"); err != nil {
		t.Fatal(err)
	}
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
	foreign, _, err := s.Register(ctx, createRequest("foreign@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTrapPermission(ctx, actor, foreign.User.ID, true, 1); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("foreign target: %v", err)
	}
	if err := s.SetTrapPermission(ctx, actor, owner.User.ID, false, 1); !errors.Is(err, auth.ErrPermissionProtected) {
		t.Fatalf("owner: %v", err)
	}
	forged := actor
	forged.OrganizationID = foreign.Organization.ID
	if _, err := repo.Members(ctx, forged, 50, ""); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("foreign actor: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.SetTrapPermission(ctx, actor, viewer.User.ID, true, 1) })
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, auth.ErrPermissionChanged) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	resolved, err := s.ResolveSession(ctx, viewerRaw)
	if err != nil || !resolved.AuthContext().CanManageTraps {
		t.Fatalf("existing session grant: %v", err)
	}
	// The actor must really be the creator, even if an admin principal is forged.
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'admin' WHERE id = $1`, viewer.User.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err = s.ResolveSession(ctx, viewerRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdminAccess(ctx, resolved, "demo-password-2026"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTrapPermission(ctx, resolved.AuthContext(), viewer.User.ID, false, 2); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("non-owner admin: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'viewer' WHERE id = $1`, viewer.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE auth_audit ADD CONSTRAINT reject_permission_audit CHECK (action <> 'member.trap_management_revoked') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTrapPermission(ctx, actor, viewer.User.ID, false, 2); err == nil {
		t.Fatal("audit failure accepted")
	}
	resolved, err = s.ResolveSession(ctx, viewerRaw)
	if err != nil || !resolved.AuthContext().CanManageTraps {
		t.Fatal("audit failure changed rights")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE auth_audit DROP CONSTRAINT reject_permission_audit`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTrapPermission(ctx, actor, viewer.User.ID, false, 2); err != nil {
		t.Fatal(err)
	}
	resolved, err = s.ResolveSession(ctx, viewerRaw)
	if err != nil || resolved.AuthContext().CanManageTraps {
		t.Fatal("revocation not applied")
	}
	if err := s.TouchPresence(ctx, resolved); err != nil {
		t.Fatal(err)
	}
	assertPresence := func(want bool) {
		t.Helper()
		page, err := s.Members(ctx, actor, 50, "")
		if err != nil || len(page.Items) != 2 || page.Creator == nil || page.Creator.ID != owner.User.ID {
			t.Fatalf("members=%+v err=%v", page, err)
		}
		for _, member := range page.Items {
			if member.ID == viewer.User.ID && member.Online != want {
				t.Fatalf("online=%v want=%v", member.Online, want)
			}
		}
	}
	assertPresence(true)
	if _, err := pool.Exec(ctx, `UPDATE operator_sessions SET last_seen_at = now() - interval '91 seconds' WHERE user_id = $1`, viewer.User.ID); err != nil {
		t.Fatal(err)
	}
	assertPresence(false)
	// Another browser is independent; logging out of one must not hide the other.
	otherView, otherRaw, err := s.Login(ctx, auth.LoginRequest{Email: viewer.User.Email, Password: "demo-password-2026"}, "")
	if err != nil {
		t.Fatal(err)
	}
	otherSess, err := s.ResolveSession(ctx, otherRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchPresence(ctx, otherSess); err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(ctx, viewerRaw, viewer.CSRFToken); err != nil {
		t.Fatal(err)
	}
	assertPresence(true)
	if err := s.TouchPresence(ctx, resolved); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("revoked presence: %v", err)
	}
	if err := s.Logout(ctx, otherRaw, otherView.CSRFToken); err != nil {
		t.Fatal(err)
	}
	assertPresence(false)
	// Creator identity survives removal of registration history.
	if _, err := pool.Exec(ctx, `DELETE FROM auth_audit WHERE action = 'organization.created'`); err != nil {
		t.Fatal(err)
	}
	assertPresence(false)
	if _, err := pool.Exec(ctx, `UPDATE organizations SET creator_user_id = $1 WHERE id = $2`, foreign.User.ID, owner.Organization.ID); err == nil {
		t.Fatal("cross-organization creator accepted")
	}
	page, err := s.Members(ctx, actor, 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("first page")
	}
	next, err := s.Members(ctx, actor, 1, *page.NextCursor)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.NextCursor != nil {
		t.Fatal("next page")
	}
}
