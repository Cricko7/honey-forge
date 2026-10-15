// Package teststore provides the shared in-memory fixture for this feature's tests.
package teststore

import (
	"context"
	"encoding/hex"
	"math"
	"sync"
	"time"

	authcore "honey-forge/modules/auth"
)

type storedUser struct {
	user    authcore.User
	org     authcore.Organization
	hash    string
	removed bool
}

type storedOrg struct {
	org  authcore.Organization
	code authcore.JoinCode
}

type Store struct {
	mu            sync.Mutex
	users         map[string]storedUser
	orgs          map[string]storedOrg
	Sessions      map[string]authcore.Session
	Err           error
	Keys          map[string]authcore.APIKey
	KeyQuota      map[string]int
	creators      map[string]string
	revisions     map[string]int32
	presence      map[string]time.Time
	adminExpires  map[string]time.Time
	adminAttempts map[string]adminAttemptWindow
}

func New() *Store {
	return &Store{
		users:         make(map[string]storedUser),
		orgs:          make(map[string]storedOrg),
		Sessions:      make(map[string]authcore.Session),
		Keys:          make(map[string]authcore.APIKey),
		KeyQuota:      make(map[string]int),
		creators:      make(map[string]string),
		revisions:     make(map[string]int32),
		presence:      make(map[string]time.Time),
		adminExpires:  make(map[string]time.Time),
		adminAttempts: make(map[string]adminAttemptWindow),
	}
}

func (m *Store) Register(_ context.Context, user authcore.User, org authcore.Organization, input authcore.OrganizationInput, hash string, sess authcore.Session, code authcore.JoinCode) (authcore.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Err != nil {
		return authcore.Organization{}, m.Err
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
		m.creators[org.ID] = user.ID
	}

	m.users[user.Email] = storedUser{user: user, org: org, hash: hash}
	m.revisions[user.ID] = 1
	m.Sessions[hex.EncodeToString(sess.Hash)] = sess

	return org, nil
}

func (m *Store) Credentials(_ context.Context, email string) (authcore.User, authcore.Organization, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Err != nil {
		return authcore.User{}, authcore.Organization{}, "", m.Err
	}

	record, ok := m.users[email]
	if !ok || record.removed {
		return authcore.User{}, authcore.Organization{}, "", authcore.ErrNotFound
	}

	return record.user, record.org, record.hash, nil
}

func (m *Store) CreateSession(_ context.Context, sess authcore.Session, old []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Err != nil {
		return m.Err
	}

	active := false
	for _, record := range m.users {
		if record.user.ID == sess.UserID && !record.removed {
			active = true
			break
		}
	}
	if !active {
		return authcore.ErrNotFound
	}
	delete(m.Sessions, hex.EncodeToString(old))
	m.Sessions[hex.EncodeToString(sess.Hash)] = sess

	return nil
}

func (m *Store) ResolveSession(_ context.Context, hash []byte) (authcore.ResolvedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Err != nil {
		return authcore.ResolvedSession{}, m.Err
	}

	sess, ok := m.Sessions[hex.EncodeToString(hash)]
	if !ok || !sess.ExpiresAt.After(time.Now()) {
		return authcore.ResolvedSession{}, authcore.ErrNotFound
	}

	for _, record := range m.users {
		if record.user.ID == sess.UserID && !record.removed {
			return authcore.ResolvedSession{
				ID:       sess.ID,
				CSRFHash: sess.CSRFHash,
				View:     authcore.SessionView{User: record.user, Organization: record.org, ExpiresAt: sess.ExpiresAt},
			}, nil
		}
	}

	return authcore.ResolvedSession{}, authcore.ErrNotFound
}

func (m *Store) RevokeSession(_ context.Context, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Err != nil {
		return m.Err
	}

	delete(m.Sessions, hex.EncodeToString(hash))

	return nil
}

func (m *Store) Organization(_ context.Context, id string) (authcore.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.orgs[id].org, m.Err
}

func (m *Store) JoinCode(_ context.Context, id string) (authcore.JoinCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.orgs[id].code, m.Err
}

func (m *Store) RotateJoinCode(_ context.Context, actor authcore.AuthContext, expected int32, next authcore.JoinCode) (authcore.JoinCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Err != nil {
		return authcore.JoinCode{}, m.Err
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
