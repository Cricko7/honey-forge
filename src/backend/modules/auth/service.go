package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrEmailTaken           = errors.New("email already registered")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUnauthorized         = errors.New("unauthenticated")
	ErrAlreadyAuthenticated = errors.New("already authenticated")
	ErrInvalidJoinCode      = errors.New("invalid organization join code")
	ErrForbidden            = errors.New("insufficient permissions")
	ErrCSRF                 = errors.New("CSRF validation failed")
	ErrJoinCodeChanged      = errors.New("organization join code changed")
	ErrRevisionExhausted    = errors.New("revision exhausted")
	ErrNotFound             = errors.New("record not found")
	ErrUnavailable          = errors.New("database unavailable")
)

type store interface {
	Register(context.Context, User, Organization, OrganizationInput, string, session, JoinCode) (Organization, error)
	Credentials(context.Context, string) (User, Organization, string, error)
	CreateSession(context.Context, session, []byte) error
	ResolveSession(context.Context, []byte) (ResolvedSession, error)
	RevokeSession(context.Context, []byte) error
	Organization(context.Context, string) (Organization, error)
	JoinCode(context.Context, string) (JoinCode, error)
	RotateJoinCode(context.Context, AuthContext, int32, JoinCode) (JoinCode, error)
}

type Service struct {
	store store
}

func NewService(repository store) *Service {
	return &Service{store: repository}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest, currentCookie string) (SessionView, string, error) {
	if err := s.CheckRegistrationSession(ctx, currentCookie); err != nil {
		return SessionView{}, "", err
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		return SessionView{}, "", err
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	org := Organization{ID: newUUID(), Name: strings.TrimSpace(req.Organization.Name), CreatedAt: now}
	user := User{ID: newUUID(), OrganizationID: org.ID, Email: normalizeEmail(req.Email), Role: RoleAdmin, CreatedAt: now}
	if req.Organization.Mode == "join" {
		user.Role = RoleViewer
	}

	sess, raw, csrf := newSession(user.ID, now)
	code := JoinCode{Code: newOpaque(24), Revision: 1, RotatedAt: now}
	org, err = s.store.Register(ctx, user, org, *req.Organization, hash, sess, code)
	if err != nil {
		return SessionView{}, "", fmt.Errorf("registering operator: %w", err)
	}

	user.OrganizationID = org.ID

	return SessionView{User: user, Organization: org, ExpiresAt: sess.ExpiresAt, CSRFToken: csrf}, raw, nil
}

func (s *Service) CheckRegistrationSession(ctx context.Context, currentCookie string) error {
	if currentCookie != "" {
		_, err := s.ResolveSession(ctx, currentCookie)
		if err == nil {
			return ErrAlreadyAuthenticated
		}
		if !errors.Is(err, ErrUnauthorized) {
			return err
		}
	}

	return nil
}

func (s *Service) Login(ctx context.Context, req LoginRequest, currentCookie string) (SessionView, string, error) {
	user, org, hash, err := s.store.Credentials(ctx, normalizeEmail(req.Email))
	if errors.Is(err, ErrNotFound) {
		dummyPasswordCheck(req.Password)

		return SessionView{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return SessionView{}, "", fmt.Errorf("loading credentials: %w", err)
	}

	matches, err := checkPassword(hash, req.Password)
	if err != nil {
		return SessionView{}, "", fmt.Errorf("checking password: %w", err)
	}
	if !matches {
		return SessionView{}, "", ErrInvalidCredentials
	}

	sess, raw, csrf := newSession(user.ID, time.Now().UTC().Truncate(time.Microsecond))
	var oldHash []byte
	if validOpaque(currentCookie, 43) {
		oldHash = hashToken(currentCookie)
	}

	if err := s.store.CreateSession(ctx, sess, oldHash); err != nil {
		return SessionView{}, "", fmt.Errorf("replacing browser session: %w", err)
	}

	return SessionView{User: user, Organization: org, ExpiresAt: sess.ExpiresAt, CSRFToken: csrf}, raw, nil
}

func (s *Service) ResolveSession(ctx context.Context, raw string) (ResolvedSession, error) {
	if !validOpaque(raw, 43) {
		return ResolvedSession{}, ErrUnauthorized
	}

	sess, err := s.store.ResolveSession(ctx, hashToken(raw))
	if errors.Is(err, ErrNotFound) {
		return ResolvedSession{}, ErrUnauthorized
	}
	if err != nil {
		return ResolvedSession{}, fmt.Errorf("resolving session: %w", err)
	}

	sess.View.CSRFToken = csrfToken(raw)

	return sess, nil
}

func (s *Service) Logout(ctx context.Context, raw, csrf string) error {
	sess, err := s.ResolveSession(ctx, raw)
	if errors.Is(err, ErrUnauthorized) {
		return nil
	}
	if err != nil {
		return err
	}

	if err := CheckCSRF(sess, csrf); err != nil {
		return err
	}

	if err := s.store.RevokeSession(ctx, hashToken(raw)); err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}

	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
