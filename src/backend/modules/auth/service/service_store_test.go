package service

import (
	"context"
	"encoding/hex"
	authcore "honey-forge/src/backend/modules/auth"
	"math"
	"sync"
	"time"
)

type storedUser struct {
	user authcore.User
	org  authcore.Organization
	hash string
}

type storedOrg struct {
	org  authcore.Organization
	code authcore.JoinCode
}

type memoryStore struct {
	mu       sync.Mutex
	users    map[string]storedUser
	orgs     map[string]storedOrg
	sessions map[string]authcore.Session
	err      error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:    make(map[string]storedUser),
		orgs:     make(map[string]storedOrg),
		sessions: make(map[string]authcore.Session),
	}
}

func (m *memoryStore) Register(_ context.Context, user authcore.User, org authcore.Organization, input authcore.OrganizationInput, hash string, sess authcore.Session, code authcore.JoinCode) (authcore.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return authcore.Organization{}, m.err
	}
	if _, exists := m.users[user.Email]; exists {
		return authcore.Organization{}, authcore.ErrEmailTaken
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
			return authcore.Organization{}, authcore.ErrInvalidJoinCode
		}

		user.OrganizationID = org.ID
	} else {
		m.orgs[org.ID] = storedOrg{org, code}
	}

	m.users[user.Email] = storedUser{user, org, hash}
	m.sessions[hex.EncodeToString(sess.Hash)] = sess

	return org, nil
}

func (m *memoryStore) Credentials(_ context.Context, email string) (authcore.User, authcore.Organization, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return authcore.User{}, authcore.Organization{}, "", m.err
	}
	record, ok := m.users[email]
	if !ok {
		return authcore.User{}, authcore.Organization{}, "", authcore.ErrNotFound
	}

	return record.user, record.org, record.hash, nil
}

func (m *memoryStore) CreateSession(_ context.Context, sess authcore.Session, old []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	delete(m.sessions, hex.EncodeToString(old))
	m.sessions[hex.EncodeToString(sess.Hash)] = sess

	return nil
}

func (m *memoryStore) ResolveSession(_ context.Context, hash []byte) (authcore.ResolvedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return authcore.ResolvedSession{}, m.err
	}
	sess, ok := m.sessions[hex.EncodeToString(hash)]
	if !ok || !sess.ExpiresAt.After(time.Now()) {
		return authcore.ResolvedSession{}, authcore.ErrNotFound
	}

	for _, record := range m.users {
		if record.user.ID == sess.UserID {
			return authcore.ResolvedSession{
				ID: sess.ID, CSRFHash: sess.CSRFHash,
				View: authcore.SessionView{User: record.user, Organization: record.org, ExpiresAt: sess.ExpiresAt},
			}, nil
		}
	}

	return authcore.ResolvedSession{}, authcore.ErrNotFound
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

func (m *memoryStore) Organization(_ context.Context, id string) (authcore.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.orgs[id].org, m.err
}

func (m *memoryStore) JoinCode(_ context.Context, id string) (authcore.JoinCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.orgs[id].code, m.err
}

func (m *memoryStore) RotateJoinCode(_ context.Context, actor authcore.AuthContext, expected int32, next authcore.JoinCode) (authcore.JoinCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return authcore.JoinCode{}, m.err
	}

	record := m.orgs[actor.OrganizationID]
	if record.code.Revision != expected {
		return authcore.JoinCode{}, authcore.ErrJoinCodeChanged
	}
	if expected == math.MaxInt32 {
		return authcore.JoinCode{}, authcore.ErrRevisionExhausted
	}

	next.Revision = expected + 1
	record.code = next
	m.orgs[actor.OrganizationID] = record

	return next, nil
}
