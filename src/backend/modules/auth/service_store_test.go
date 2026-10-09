package auth

import (
	"context"
	"encoding/hex"
	"math"
	"sync"
	"time"
)

type storedUser struct {
	user User
	org  Organization
	hash string
}

type storedOrg struct {
	org  Organization
	code JoinCode
}

type memoryStore struct {
	mu       sync.Mutex
	users    map[string]storedUser
	orgs     map[string]storedOrg
	sessions map[string]session
	err      error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:    make(map[string]storedUser),
		orgs:     make(map[string]storedOrg),
		sessions: make(map[string]session),
	}
}

func (m *memoryStore) Register(_ context.Context, user User, org Organization, input OrganizationInput, hash string, sess session, code JoinCode) (Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return Organization{}, m.err
	}
	if _, exists := m.users[user.Email]; exists {
		return Organization{}, ErrEmailTaken
	}

	if input.Mode == "join" {
		found := false
		for _, candidate := range m.orgs {
			if candidate.code.Code == input.JoinCode {
				org, found = candidate.org, true
				break
			}
		}
		if !found {
			return Organization{}, ErrInvalidJoinCode
		}

		user.OrganizationID = org.ID
	} else {
		m.orgs[org.ID] = storedOrg{org, code}
	}

	m.users[user.Email] = storedUser{user, org, hash}
	m.sessions[hex.EncodeToString(sess.Hash)] = sess

	return org, nil
}

func (m *memoryStore) Credentials(_ context.Context, email string) (User, Organization, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return User{}, Organization{}, "", m.err
	}
	record, ok := m.users[email]
	if !ok {
		return User{}, Organization{}, "", ErrNotFound
	}

	return record.user, record.org, record.hash, nil
}

func (m *memoryStore) CreateSession(_ context.Context, sess session, old []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	delete(m.sessions, hex.EncodeToString(old))
	m.sessions[hex.EncodeToString(sess.Hash)] = sess

	return nil
}

func (m *memoryStore) ResolveSession(_ context.Context, hash []byte) (ResolvedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return ResolvedSession{}, m.err
	}
	sess, ok := m.sessions[hex.EncodeToString(hash)]
	if !ok || !sess.ExpiresAt.After(time.Now()) {
		return ResolvedSession{}, ErrNotFound
	}

	for _, record := range m.users {
		if record.user.ID == sess.UserID {
			return ResolvedSession{
				ID: sess.ID, csrfHash: sess.CSRFHash,
				View: SessionView{User: record.user, Organization: record.org, ExpiresAt: sess.ExpiresAt},
			}, nil
		}
	}

	return ResolvedSession{}, ErrNotFound
}

func (m *memoryStore) RevokeSession(_ context.Context, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	delete(m.sessions, hex.EncodeToString(hash))

	return nil
}

func (m *memoryStore) Organization(_ context.Context, id string) (Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.orgs[id].org, m.err
}

func (m *memoryStore) JoinCode(_ context.Context, id string) (JoinCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.orgs[id].code, m.err
}

func (m *memoryStore) RotateJoinCode(_ context.Context, actor AuthContext, expected int32, next JoinCode) (JoinCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return JoinCode{}, m.err
	}

	record := m.orgs[actor.OrganizationID]
	if record.code.Revision != expected {
		return JoinCode{}, ErrJoinCodeChanged
	}
	if expected == math.MaxInt32 {
		return JoinCode{}, ErrRevisionExhausted
	}

	next.Revision = expected + 1
	record.code = next
	m.orgs[actor.OrganizationID] = record

	return next, nil
}
