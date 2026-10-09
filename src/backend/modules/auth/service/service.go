package service

import (
	"context"
	"errors"
	"fmt"
	authcore "github.com/Cricko7/honey-forge/src/backend/modules/auth"
	"strings"
	"time"
)

type store interface {
	Register(context.Context, authcore.User, authcore.Organization, authcore.OrganizationInput, string, authcore.Session, authcore.JoinCode) (authcore.Organization, error)
	Credentials(context.Context, string) (authcore.User, authcore.Organization, string, error)
	CreateSession(context.Context, authcore.Session, []byte) error
	ResolveSession(context.Context, []byte) (authcore.ResolvedSession, error)
	RevokeSession(context.Context, []byte) error
	Organization(context.Context, string) (authcore.Organization, error)
	JoinCode(context.Context, string) (authcore.JoinCode, error)
	RotateJoinCode(context.Context, authcore.AuthContext, int32, authcore.JoinCode) (authcore.JoinCode, error)
}

type Service struct {
	store store
}

func NewService(repository store) *Service {
	return &Service{store: repository}
}

func (s *Service) Register(ctx context.Context, req authcore.RegisterRequest, currentCookie string) (authcore.SessionView, string, error) {
	if err := s.CheckRegistrationSession(ctx, currentCookie); err != nil {
		return authcore.SessionView{}, "", err
	}

	hash, err := authcore.HashPassword(req.Password)
	if err != nil {
		return authcore.SessionView{}, "", err
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	org := authcore.Organization{ID: authcore.NewUUID(), Name: strings.TrimSpace(req.Organization.Name), CreatedAt: now}
	user := authcore.User{ID: authcore.NewUUID(), OrganizationID: org.ID, Email: normalizeEmail(req.Email), Role: authcore.RoleAdmin, CreatedAt: now}
	if req.Organization.Mode == "join" {
		user.Role = authcore.RoleViewer
	}

	sess, raw, csrf := authcore.NewSession(user.ID, now)
	code := authcore.JoinCode{Code: authcore.NewOpaque(24), Revision: 1, RotatedAt: now}
	org, err = s.store.Register(ctx, user, org, *req.Organization, hash, sess, code)
	if err != nil {
		return authcore.SessionView{}, "", fmt.Errorf("registering operator: %w", err)
	}

	user.OrganizationID = org.ID

	return authcore.SessionView{User: user, Organization: org, ExpiresAt: sess.ExpiresAt, CSRFToken: csrf}, raw, nil
}

func (s *Service) CheckRegistrationSession(ctx context.Context, currentCookie string) error {
	if currentCookie != "" {
		_, err := s.ResolveSession(ctx, currentCookie)
		if err == nil {
			return authcore.ErrAlreadyAuthenticated
		}
		if !errors.Is(err, authcore.ErrUnauthorized) {
			return err
		}
	}

	return nil
}

func (s *Service) Login(ctx context.Context, req authcore.LoginRequest, currentCookie string) (authcore.SessionView, string, error) {
	user, org, hash, err := s.store.Credentials(ctx, normalizeEmail(req.Email))
	if errors.Is(err, authcore.ErrNotFound) {
		authcore.DummyPasswordCheck(req.Password)

		return authcore.SessionView{}, "", authcore.ErrInvalidCredentials
	}
	if err != nil {
		return authcore.SessionView{}, "", fmt.Errorf("loading credentials: %w", err)
	}

	matches, err := authcore.CheckPassword(hash, req.Password)
	if err != nil {
		return authcore.SessionView{}, "", fmt.Errorf("checking password: %w", err)
	}
	if !matches {
		return authcore.SessionView{}, "", authcore.ErrInvalidCredentials
	}

	sess, raw, csrf := authcore.NewSession(user.ID, time.Now().UTC().Truncate(time.Microsecond))
	var oldHash []byte
	if authcore.ValidOpaque(currentCookie, 43) {
		oldHash = authcore.HashToken(currentCookie)
	}

	if err := s.store.CreateSession(ctx, sess, oldHash); err != nil {
		return authcore.SessionView{}, "", fmt.Errorf("replacing browser session: %w", err)
	}

	return authcore.SessionView{User: user, Organization: org, ExpiresAt: sess.ExpiresAt, CSRFToken: csrf}, raw, nil
}

func (s *Service) ResolveSession(ctx context.Context, raw string) (authcore.ResolvedSession, error) {
	if !authcore.ValidOpaque(raw, 43) {
		return authcore.ResolvedSession{}, authcore.ErrUnauthorized
	}

	sess, err := s.store.ResolveSession(ctx, authcore.HashToken(raw))
	if errors.Is(err, authcore.ErrNotFound) {
		return authcore.ResolvedSession{}, authcore.ErrUnauthorized
	}
	if err != nil {
		return authcore.ResolvedSession{}, fmt.Errorf("resolving session: %w", err)
	}

	sess.View.CSRFToken = authcore.CSRFToken(raw)

	return sess, nil
}

func (s *Service) Logout(ctx context.Context, raw, csrf string) error {
	sess, err := s.ResolveSession(ctx, raw)
	if errors.Is(err, authcore.ErrUnauthorized) {
		return nil
	}
	if err != nil {
		return err
	}

	if err := authcore.CheckCSRF(sess, csrf); err != nil {
		return err
	}

	if err := s.store.RevokeSession(ctx, authcore.HashToken(raw)); err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}

	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
