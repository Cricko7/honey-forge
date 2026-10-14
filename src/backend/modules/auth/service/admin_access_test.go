package service

import (
	"errors"
	"testing"

	"honey-forge/modules/auth"
	"honey-forge/modules/auth/internal/teststore"
)

func TestAdminAccessRequiresPasswordForEachBrowserSession(t *testing.T) {
	repo := teststore.New()
	s := NewService(repo)
	_, raw, err := s.Register(t.Context(), createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.ResolveSession(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	actor := sess.AuthContext()
	if _, err := s.Members(t.Context(), actor, 50, ""); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatalf("unconfirmed access: %v", err)
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), sess, "incorrect-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	view, err := s.ConfirmAdminAccess(t.Context(), sess, "demo-password-2026")
	if err != nil || view.ExpiresAt == nil || !view.ExpiresAt.Before(sess.View.ExpiresAt) {
		t.Fatalf("confirmation=%+v err=%v", view, err)
	}
	if _, err := s.Members(t.Context(), actor, 50, ""); err != nil {
		t.Fatal(err)
	}
	_, otherRaw, err := s.Login(t.Context(), auth.LoginRequest{Email: sess.View.User.Email, Password: "demo-password-2026"}, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.ResolveSession(t.Context(), otherRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Members(t.Context(), other.AuthContext(), 50, ""); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatalf("another browser inherited confirmation: %v", err)
	}
	if err := s.LockAdminAccess(t.Context(), actor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Members(t.Context(), actor, 50, ""); !errors.Is(err, auth.ErrAdminConfirmationRequired) {
		t.Fatalf("lock failed: %v", err)
	}
	if err := s.Logout(t.Context(), raw, sess.View.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), sess, "demo-password-2026"); err == nil {
		t.Fatal("revoked session confirmed")
	}
}

func TestAdminPasswordAttemptsAreBoundedAcrossSessions(t *testing.T) {
	repo := teststore.New()
	s := NewService(repo)
	_, raw, err := s.Register(t.Context(), createRequest("owner@example.com"), "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.ResolveSession(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := s.ConfirmAdminAccess(t.Context(), sess, "incorrect-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	_, otherRaw, err := s.Login(t.Context(), auth.LoginRequest{Email: sess.View.User.Email, Password: "demo-password-2026"}, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.ResolveSession(t.Context(), otherRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), other, "demo-password-2026"); err == nil {
		t.Fatal("another session bypassed attempt quota")
	}
	code, err := s.JoinCode(t.Context(), sess.AuthContext())
	if err != nil {
		t.Fatal(err)
	}
	join := createRequest("viewer@example.com")
	join.Organization = &auth.OrganizationInput{Mode: "join", JoinCode: code.Code}
	_, viewerRaw, err := s.Register(t.Context(), join, "")
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := s.ResolveSession(t.Context(), viewerRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdminAccess(t.Context(), viewer, "demo-password-2026"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("viewer confirmation: %v", err)
	}
}
