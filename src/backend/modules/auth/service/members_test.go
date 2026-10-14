package service

import (
	"errors"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/auth/internal/teststore"
)

func TestMembersAndTrapPermissionLifecycle(t *testing.T) {
	repo := teststore.New()
	s := NewService(repo)
	admin, raw, err := s.Register(t.Context(), createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.ResolveSession(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	actor := sess.AuthContext()
	if _, err := s.ConfirmAdminAccess(t.Context(), sess, "demo-password-2026"); err != nil {
		t.Fatal(err)
	}
	code, err := s.JoinCode(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	join := createRequest("viewer@example.com")
	join.Organization = &auth.OrganizationInput{Mode: "join", JoinCode: code.Code}
	viewer, viewerRaw, err := s.Register(t.Context(), join, "")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.Register(t.Context(), createRequest("other@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Members(t.Context(), actor, 50, "")
	if err != nil || len(page.Items) != 2 || page.Creator == nil || page.Creator.ID != admin.User.ID {
		t.Fatalf("members=%+v error=%v", page, err)
	}
	for _, denied := range []auth.AuthContext{{}, {UserID: viewer.User.ID, OrganizationID: actor.OrganizationID, Role: auth.RoleViewer, CanManageTraps: true}} {
		if _, err := s.Members(t.Context(), denied, 50, ""); err == nil {
			t.Fatal("unauthorized member list")
		}
		if err := s.SetTrapPermission(t.Context(), denied, viewer.User.ID, true, 1); err == nil {
			t.Fatal("self promotion accepted")
		}
	}
	if err := s.SetTrapPermission(t.Context(), actor, other.User.ID, true, 1); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("foreign target: %v", err)
	}
	if err := s.SetTrapPermission(t.Context(), actor, admin.User.ID, false, 1); !errors.Is(err, auth.ErrPermissionProtected) {
		t.Fatalf("owner protection: %v", err)
	}
	if err := s.SetTrapPermission(t.Context(), actor, viewer.User.ID, true, 1); err != nil {
		t.Fatal(err)
	}
	granted, err := s.ResolveSession(t.Context(), viewerRaw)
	if err != nil || !granted.AuthContext().CanManageTraps || granted.View.User.Role != auth.RoleViewer {
		t.Fatal("grant changed role or not visible to existing session")
	}
	if err := s.SetTrapPermission(t.Context(), actor, viewer.User.ID, false, 1); !errors.Is(err, auth.ErrPermissionChanged) {
		t.Fatalf("stale revision: %v", err)
	}
	if err := s.SetTrapPermission(t.Context(), actor, viewer.User.ID, false, 2); err != nil {
		t.Fatal(err)
	}
	revoked, err := s.ResolveSession(t.Context(), viewerRaw)
	if err != nil || revoked.AuthContext().CanManageTraps {
		t.Fatal("revocation not visible to existing session")
	}
	repo.Err = errors.New("storage failed")
	if _, err := s.Members(t.Context(), actor, 50, ""); !errors.Is(err, repo.Err) {
		t.Fatalf("storage failure: %v", err)
	}
}
