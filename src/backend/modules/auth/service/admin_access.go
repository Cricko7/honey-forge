package service

import (
	"context"
	"fmt"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
)

func (s *Service) AdminAccess(ctx context.Context, actor auth.AuthContext) (auth.AdminAccessView, error) {
	if err := authorizeMembers(actor); err != nil {
		return auth.AdminAccessView{}, err
	}
	if !contract.ValidID(actor.SessionID) {
		return auth.AdminAccessView{}, auth.ErrAdminConfirmationRequired
	}
	view, err := s.store.AdminAccess(ctx, actor)
	if err != nil {
		return auth.AdminAccessView{}, fmt.Errorf("checking admin confirmation: %w", err)
	}
	return view, nil
}

func (s *Service) RequireAdminAccess(ctx context.Context, actor auth.AuthContext) error {
	view, err := s.AdminAccess(ctx, actor)
	if err != nil {
		return err
	}
	if view.ExpiresAt == nil {
		return auth.ErrAdminConfirmationRequired
	}
	return nil
}

func (s *Service) ConfirmAdminAccess(ctx context.Context, sess auth.ResolvedSession, password string) (auth.AdminAccessView, error) {
	actor := sess.AuthContext()
	if _, err := s.AdminAccess(ctx, actor); err != nil {
		return auth.AdminAccessView{}, err
	}
	if err := s.store.ConsumeAdminAttempt(ctx, actor); err != nil {
		return auth.AdminAccessView{}, fmt.Errorf("consuming admin password attempt: %w", err)
	}
	user, _, hash, err := s.store.Credentials(ctx, sess.View.User.Email)
	if err != nil {
		return auth.AdminAccessView{}, fmt.Errorf("loading admin password: %w", err)
	}
	if user.ID != actor.UserID || user.OrganizationID != actor.OrganizationID || user.Role != auth.RoleAdmin {
		return auth.AdminAccessView{}, auth.ErrForbidden
	}
	matches, err := auth.CheckPassword(hash, password)
	if err != nil {
		return auth.AdminAccessView{}, fmt.Errorf("checking admin password: %w", err)
	}
	if !matches {
		return auth.AdminAccessView{}, auth.ErrInvalidCredentials
	}
	view, err := s.store.ConfirmAdminAccess(ctx, actor, hash)
	if err != nil {
		return auth.AdminAccessView{}, fmt.Errorf("confirming admin browser session: %w", err)
	}
	return view, nil
}

func (s *Service) LockAdminAccess(ctx context.Context, actor auth.AuthContext) error {
	if _, err := s.AdminAccess(ctx, actor); err != nil {
		return err
	}
	if err := s.store.LockAdminAccess(ctx, actor); err != nil {
		return fmt.Errorf("locking admin browser session: %w", err)
	}
	return nil
}
