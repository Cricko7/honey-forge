package teststore

import (
	"bytes"
	"context"

	authcore "honey-forge/modules/auth"
)

func (m *Store) CreateAPIKey(_ context.Context, key authcore.APIKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}

	active := false
	for _, record := range m.users {
		if record.user.ID == key.UserID && !record.removed {
			active = true
			break
		}
	}
	if !active {
		return authcore.ErrNotFound
	}
	m.Keys[key.ID] = key

	return nil
}

func (m *Store) ListAPIKeys(_ context.Context, organizationID string) ([]authcore.APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := []authcore.APIKey{}
	for _, key := range m.Keys {
		for _, user := range m.users {
			if user.user.ID == key.UserID && user.org.ID == organizationID {
				keys = append(keys, key)
			}
		}
	}

	return keys, m.Err
}

func (m *Store) RevokeAPIKey(_ context.Context, actor authcore.AuthContext, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.Keys[id]
	if !ok {
		return authcore.ErrNotFound
	}

	for _, user := range m.users {
		if user.user.ID == key.UserID && user.org.ID == actor.OrganizationID {
			delete(m.Keys, id)
			delete(m.KeyQuota, id)

			return nil
		}
	}

	return authcore.ErrNotFound
}

func (m *Store) ResolveAPIKey(_ context.Context, hash []byte) (authcore.ResolvedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return authcore.ResolvedSession{}, m.Err
	}

	for _, key := range m.Keys {
		if !bytes.Equal(key.Hash, hash) {
			continue
		}

		for _, user := range m.users {
			if user.user.ID == key.UserID && !user.removed {
				return authcore.ResolvedSession{ID: key.ID, View: authcore.SessionView{User: user.user, Organization: user.org}}, nil
			}
		}
	}

	return authcore.ResolvedSession{}, authcore.ErrNotFound
}

func (m *Store) ConsumeAPIKey(_ context.Context, id string, limit int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Keys[id]; !ok {
		return authcore.ErrNotFound
	}

	if m.KeyQuota[id] >= limit {
		return &authcore.APIKeyRateError{RetryAfter: 60}
	}

	m.KeyQuota[id]++

	return nil
}
