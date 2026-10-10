package service

import (
	"context"
	"fmt"
	authcore "honey-forge/modules/auth"
	"time"
)

func (s *Service) Organization(ctx context.Context, actor authcore.AuthContext) (authcore.Organization, error) {
	if actor.UserID == "" || actor.OrganizationID == "" {
		return authcore.Organization{}, authcore.ErrUnauthorized
	}

	org, err := s.store.Organization(ctx, actor.OrganizationID)
	if err != nil {
		return authcore.Organization{}, fmt.Errorf("loading own organization: %w", err)
	}

	return org, nil
}

func (s *Service) JoinCode(ctx context.Context, actor authcore.AuthContext) (authcore.JoinCode, error) {
	if actor.UserID == "" || actor.OrganizationID == "" {
		return authcore.JoinCode{}, authcore.ErrUnauthorized
	}
	if actor.Role != authcore.RoleAdmin {
		return authcore.JoinCode{}, authcore.ErrForbidden
	}

	code, err := s.store.JoinCode(ctx, actor.OrganizationID)
	if err != nil {
		return authcore.JoinCode{}, fmt.Errorf("loading organization join code: %w", err)
	}

	return code, nil
}

func (s *Service) RotateJoinCode(ctx context.Context, actor authcore.AuthContext, expected int32) (authcore.JoinCode, error) {
	if actor.UserID == "" || actor.OrganizationID == "" {
		return authcore.JoinCode{}, authcore.ErrUnauthorized
	}
	if actor.Role != authcore.RoleAdmin {
		return authcore.JoinCode{}, authcore.ErrForbidden
	}

	next := authcore.JoinCode{Code: authcore.NewOpaque(24), RotatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	code, err := s.store.RotateJoinCode(ctx, actor, expected, next)
	if err != nil {
		return authcore.JoinCode{}, fmt.Errorf("rotating organization join code: %w", err)
	}

	return code, nil
}
