package teststore

import (
	"context"
	"time"

	"honey-forge/modules/auth"
)

func (m *Store) RemoveMember(_ context.Context, actor auth.AuthContext, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if !m.memberAdmin(actor) || m.creators[actor.OrganizationID] != actor.UserID {
		return auth.ErrForbidden
	}
	if _, err := m.adminSession(actor); err != nil {
		return err
	}
	if !m.adminExpires[actor.SessionID].After(time.Now()) {
		return auth.ErrAdminConfirmationRequired
	}
	for email, rec := range m.users {
		if rec.user.ID != id || rec.user.OrganizationID != actor.OrganizationID || rec.removed {
			continue
		}
		if id == actor.UserID || id == m.creators[actor.OrganizationID] || rec.user.Role == auth.RoleAdmin {
			return auth.ErrMemberProtected
		}
		rec.removed = true
		rec.user.CanManageTraps = false
		m.users[email] = rec
		for hash, sess := range m.Sessions {
			if sess.UserID == id {
				delete(m.Sessions, hash)
				delete(m.presence, sess.ID)
				delete(m.adminExpires, sess.ID)
			}
		}
		for keyID, key := range m.Keys {
			if key.UserID == id {
				delete(m.Keys, keyID)
				delete(m.KeyQuota, keyID)
			}
		}
		return nil
	}
	return auth.ErrNotFound
}
