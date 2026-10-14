package teststore

import (
	"context"
	"time"

	"honey-forge/modules/auth"
)

type adminAttemptWindow struct {
	count   int
	started time.Time
}

// Caller holds mu; confirmation must match both the active session and its user.
func (m *Store) adminSession(actor auth.AuthContext) (auth.Session, error) {
	if !m.memberAdmin(actor) {
		return auth.Session{}, auth.ErrForbidden
	}
	for _, sess := range m.Sessions {
		if sess.ID == actor.SessionID && sess.UserID == actor.UserID && sess.ExpiresAt.After(time.Now()) {
			return sess, nil
		}
	}
	return auth.Session{}, auth.ErrUnauthorized
}

func (m *Store) AdminAccess(_ context.Context, actor auth.AuthContext) (auth.AdminAccessView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return auth.AdminAccessView{}, m.Err
	}
	if _, err := m.adminSession(actor); err != nil {
		return auth.AdminAccessView{}, err
	}
	view := auth.AdminAccessView{}
	if expires := m.adminExpires[actor.SessionID]; expires.After(time.Now()) {
		view.ExpiresAt = &expires
	}
	return view, nil
}

func (m *Store) ConsumeAdminAttempt(_ context.Context, actor auth.AuthContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if _, err := m.adminSession(actor); err != nil {
		return err
	}
	now := time.Now()
	window := m.adminAttempts[actor.UserID]
	if !window.started.Add(15 * time.Minute).After(now) {
		window = adminAttemptWindow{started: now}
	}
	if window.count >= 5 {
		return &auth.AdminPasswordRateError{RetryAfter: 900}
	}
	window.count++
	m.adminAttempts[actor.UserID] = window
	return nil
}

func (m *Store) ConfirmAdminAccess(_ context.Context, actor auth.AuthContext, expectedHash string) (auth.AdminAccessView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return auth.AdminAccessView{}, m.Err
	}
	sess, err := m.adminSession(actor)
	if err != nil {
		return auth.AdminAccessView{}, err
	}
	for _, rec := range m.users {
		if rec.user.ID == actor.UserID && rec.hash == expectedHash {
			expires := time.Now().UTC().Add(auth.AdminAccessTTL)
			if sess.ExpiresAt.Before(expires) {
				expires = sess.ExpiresAt
			}
			m.adminExpires[actor.SessionID] = expires
			return auth.AdminAccessView{ExpiresAt: &expires}, nil
		}
	}
	return auth.AdminAccessView{}, auth.ErrUnauthorized
}

func (m *Store) LockAdminAccess(_ context.Context, actor auth.AuthContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if _, err := m.adminSession(actor); err != nil {
		return err
	}
	delete(m.adminExpires, actor.SessionID)
	return nil
}
