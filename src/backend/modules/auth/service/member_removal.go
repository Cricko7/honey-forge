package service

import (
	"context"
	"fmt"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
)

func (s *Service) RemoveMember(ctx context.Context, actor auth.AuthContext, id string) error {
	if err := s.RequireAdminAccess(ctx, actor); err != nil {
		return err
	}
	if !contract.ValidID(id) {
		return auth.ErrNotFound
	}
	// Ownership and target protection are checked under the repository's locks.
	if err := s.store.RemoveMember(ctx, actor, id); err != nil {
		return fmt.Errorf("removing organization member: %w", err)
	}
	return nil
}
