package auth

import (
	"context"
	"fmt"
	"time"
)

func (s *Service) Organization(ctx context.Context, actor AuthContext) (Organization, error) {
	if actor.UserID == "" || actor.OrganizationID == "" {
		return Organization{}, ErrUnauthorized
	}

	org, err := s.store.Organization(ctx, actor.OrganizationID)
	if err != nil {
		return Organization{}, fmt.Errorf("loading own organization: %w", err)
	}

	return org, nil
}

func (s *Service) JoinCode(ctx context.Context, actor AuthContext) (JoinCode, error) {
	if actor.UserID == "" || actor.OrganizationID == "" {
		return JoinCode{}, ErrUnauthorized
	}
	if actor.Role != RoleAdmin {
		return JoinCode{}, ErrForbidden
	}

	code, err := s.store.JoinCode(ctx, actor.OrganizationID)
	if err != nil {
		return JoinCode{}, fmt.Errorf("loading organization join code: %w", err)
	}

	return code, nil
}

func (s *Service) RotateJoinCode(ctx context.Context, actor AuthContext, expected int32) (JoinCode, error) {
	if actor.UserID == "" || actor.OrganizationID == "" {
		return JoinCode{}, ErrUnauthorized
	}
	if actor.Role != RoleAdmin {
		return JoinCode{}, ErrForbidden
	}

	next := JoinCode{Code: newOpaque(24), RotatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	code, err := s.store.RotateJoinCode(ctx, actor, expected, next)
	if err != nil {
		return JoinCode{}, fmt.Errorf("rotating organization join code: %w", err)
	}

	return code, nil
}
