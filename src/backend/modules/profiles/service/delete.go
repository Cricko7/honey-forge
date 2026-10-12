package service

import (
	"context"
	"fmt"
	"strings"

	"honey-forge/modules/auth"
	profilecore "honey-forge/modules/profiles"
)

func (s *Service) Delete(ctx context.Context, a auth.AuthContext, id, etag string) error {
	if err := Authorize(a, true); err != nil {
		return err
	}

	if s.store == nil {
		return profilecore.ErrDatabaseUnavailable
	}

	err := s.store.Delete(ctx, a, strings.ToLower(id), func(p profilecore.Profile, bindings profilecore.BindingCheck) error {
		if err := profilecore.Precondition(p, etag); err != nil {
			return err
		}

		if bindings == nil {
			return profilecore.ErrUnavailable
		}

		used, err := bindings(ctx)
		if err != nil {
			return fmt.Errorf("checking profile bindings: %w", err)
		}

		if used {
			return profilecore.ErrInUse
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("deleting profile: %w", err)
	}

	return nil
}
