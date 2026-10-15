package service

import (
	"errors"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/auth/internal/teststore"
)

func TestRemoveMember(t *testing.T) {
	repo := teststore.New()
	s := NewService(repo)
	owner, raw, err := s.Register(t.Context(), createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.ResolveSession(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	actor := sess.AuthContext()
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
	foreign, _, err := s.Register(t.Context(), createRequest("foreign@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(t.Context(), actor, viewer.User.ID); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatalf("unconfirmed admin: %v", err)
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), sess, "demo-password-2026"); err != nil {
		t.Fatal(err)
	}
	viewerSession, err := s.ResolveSession(t.Context(), viewerRaw)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		actor auth.AuthContext
		id    string
		want  error
	}{
		{"anonymous", auth.AuthContext{}, viewer.User.ID, auth.ErrUnauthorized},
		{"viewer cannot remove", viewerSession.AuthContext(), owner.User.ID, auth.ErrForbidden},
		{"creator protected", actor, owner.User.ID, auth.ErrMemberProtected},
		{"foreign target hidden", actor, foreign.User.ID, auth.ErrNotFound},
		{"absent target", actor, auth.NewUUID(), auth.ErrNotFound},
		{"invalid target", actor, "invalid", auth.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.RemoveMember(t.Context(), tc.actor, tc.id); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	_, otherRaw, err := s.Login(t.Context(), auth.LoginRequest{Email: viewer.User.Email, Password: "demo-password-2026"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Retained principals cannot create new sessions or keys after removal.
	staleSession, _, _ := auth.NewSession(viewer.User.ID, viewer.User.CreatedAt)
	if err := s.RemoveMember(t.Context(), actor, viewer.User.ID); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range []string{viewerRaw, otherRaw} {
		if _, err := s.ResolveSession(t.Context(), cookie); !errors.Is(err, auth.ErrUnauthorized) {
			t.Fatalf("removed member session: %v", err)
		}
	}
	if _, _, err := s.Login(t.Context(), auth.LoginRequest{Email: viewer.User.Email, Password: "demo-password-2026"}, ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("removed member login: %v", err)
	}
	if err := repo.CreateSession(t.Context(), staleSession, nil); err == nil {
		t.Fatal("stale login recreated a session")
	}
	page, err := s.Members(t.Context(), actor, 50, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != owner.User.ID {
		t.Fatalf("members=%+v err=%v", page, err)
	}
	if err := s.RemoveMember(t.Context(), actor, viewer.User.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("repeated removal: %v", err)
	}
	if _, err := s.ResolveSession(t.Context(), raw); err != nil {
		t.Fatalf("owner session changed: %v", err)
	}
	repo.Err = errors.New("storage unavailable")
	if err := s.RemoveMember(t.Context(), actor, foreign.User.ID); !errors.Is(err, repo.Err) {
		t.Fatalf("storage error: %v", err)
	}
}
