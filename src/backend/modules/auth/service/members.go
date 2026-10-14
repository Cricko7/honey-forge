package service

import (
	"context"
	"fmt"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
)

func authorizeMembers(actor auth.AuthContext) error {
	if !contract.ValidID(actor.UserID) || !contract.ValidID(actor.OrganizationID) {
		return auth.ErrUnauthorized
	}
	if actor.Role != auth.RoleAdmin {
		return auth.ErrForbidden
	}
	return nil
}

func (s *Service) Members(ctx context.Context, actor auth.AuthContext, limit int, cursor string) (auth.MemberPage, error) {
	if err := s.RequireAdminAccess(ctx, actor); err != nil {
		return auth.MemberPage{}, err
	}
	if limit < 1 || limit > 100 || (cursor != "" && !contract.ValidID(cursor)) {
		return auth.MemberPage{}, auth.ErrForbidden
	}
	page, err := s.store.Members(ctx, actor, limit, cursor)
	if err != nil {
		return auth.MemberPage{}, fmt.Errorf("listing organization members: %w", err)
	}
	return page, nil
}

func (s *Service) SetTrapPermission(ctx context.Context, actor auth.AuthContext, id string, granted bool, expected int32) error {
	if err := s.RequireAdminAccess(ctx, actor); err != nil {
		return err
	}
	if !contract.ValidID(id) || expected < 1 {
		return auth.ErrForbidden
	}
	if err := s.store.SetTrapPermission(ctx, actor, id, granted, expected); err != nil {
		return fmt.Errorf("setting member trap permission: %w", err)
	}
	return nil
}

func (s *Service) TouchPresence(ctx context.Context, sess auth.ResolvedSession) error {
	if !contract.ValidID(sess.ID) || !contract.ValidID(sess.View.User.ID) {
		return auth.ErrUnauthorized
	}
	if err := s.store.TouchPresence(ctx, sess.ID, sess.View.User.ID); err != nil {
		return fmt.Errorf("updating browser presence: %w", err)
	}
	return nil
}
