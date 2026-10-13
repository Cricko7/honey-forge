package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	authcore "honey-forge/modules/auth"
)

func (s *Service) WithAPIKeyRateLimit(limit int) *Service {
	if limit > 0 {
		s.apiKeyLimit = limit
	}

	return s
}

func (s *Service) CreateAPIKey(ctx context.Context, actor authcore.AuthContext, name string) (authcore.APIKey, string, error) {
	if actor.Role != authcore.RoleAdmin {
		return authcore.APIKey{}, "", authcore.ErrForbidden
	}

	raw := "hf_" + authcore.NewOpaque(32)
	key := authcore.APIKey{
		ID: authcore.NewUUID(), Name: name, Prefix: raw[:11],
		UserID: actor.UserID, Hash: authcore.HashToken(raw),
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.store.CreateAPIKey(ctx, key); err != nil {
		return authcore.APIKey{}, "", fmt.Errorf("creating API key: %w", err)
	}

	return key, raw, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, actor authcore.AuthContext) ([]authcore.APIKey, error) {
	if actor.Role != authcore.RoleAdmin {
		return nil, authcore.ErrForbidden
	}

	return s.store.ListAPIKeys(ctx, actor.OrganizationID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, actor authcore.AuthContext, id string) error {
	if actor.Role != authcore.RoleAdmin {
		return authcore.ErrForbidden
	}

	return s.store.RevokeAPIKey(ctx, actor, id)
}

// ResolveAPIKey is also used by realtime revalidation, which consumes no quota.
func (s *Service) ResolveAPIKey(ctx context.Context, raw string) (authcore.ResolvedSession, error) {
	if !strings.HasPrefix(raw, "hf_") || !authcore.ValidOpaque(strings.TrimPrefix(raw, "hf_"), 43) {
		return authcore.ResolvedSession{}, authcore.ErrUnauthorized
	}

	sess, err := s.store.ResolveAPIKey(ctx, authcore.HashToken(raw))
	if errors.Is(err, authcore.ErrNotFound) {
		return authcore.ResolvedSession{}, authcore.ErrUnauthorized
	}

	return sess, err
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, raw string) (authcore.ResolvedSession, error) {
	sess, err := s.ResolveAPIKey(ctx, raw)
	if err != nil {
		return authcore.ResolvedSession{}, err
	}

	if err := s.store.ConsumeAPIKey(ctx, sess.ID, s.apiKeyLimit); err != nil {
		if errors.Is(err, authcore.ErrNotFound) {
			return authcore.ResolvedSession{}, authcore.ErrUnauthorized
		}

		return authcore.ResolvedSession{}, err
	}

	return sess, nil
}
