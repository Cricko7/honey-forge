package teststore

import (
	"context"
	"math"
	"sort"
	"time"

	"honey-forge/modules/auth"
)

func (m *Store) memberAdmin(actor auth.AuthContext) bool {
	for _, rec := range m.users {
		if rec.user.ID == actor.UserID && rec.user.OrganizationID == actor.OrganizationID && rec.user.Role == auth.RoleAdmin {
			return true
		}
	}
	return false
}

func (m *Store) Members(_ context.Context, actor auth.AuthContext, limit int, cursor string) (auth.MemberPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return auth.MemberPage{}, m.Err
	}
	if !m.memberAdmin(actor) {
		return auth.MemberPage{}, auth.ErrForbidden
	}
	page := auth.MemberPage{Items: []auth.Member{}}
	now := time.Now()
	for _, rec := range m.users {
		u := rec.user
		if u.OrganizationID != actor.OrganizationID {
			continue
		}
		if u.ID == m.creators[actor.OrganizationID] {
			creator := u
			page.Creator = &creator
		}
		if u.ID <= cursor {
			continue
		}
		member := auth.Member{User: u, PermissionRevision: m.revisions[u.ID]}
		for _, sess := range m.Sessions {
			seen, ok := m.presence[sess.ID]
			if sess.UserID != u.ID || !sess.ExpiresAt.After(now) || !ok {
				continue
			}
			if member.LastSeenAt == nil || seen.After(*member.LastSeenAt) {
				copy := seen
				member.LastSeenAt = &copy
			}
		}
		member.Online = member.LastSeenAt != nil && member.LastSeenAt.After(now.Add(-auth.PresenceWindow))
		page.Items = append(page.Items, member)
	}
	sort.Slice(page.Items, func(i, j int) bool { return page.Items[i].ID < page.Items[j].ID })
	if len(page.Items) > limit {
		next := page.Items[limit-1].ID
		page.NextCursor = &next
		page.Items = page.Items[:limit]
	}
	return page, nil
}

func (m *Store) SetTrapPermission(_ context.Context, actor auth.AuthContext, id string, granted bool, expected int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if !m.memberAdmin(actor) || m.creators[actor.OrganizationID] != actor.UserID {
		return auth.ErrForbidden
	}
	for email, rec := range m.users {
		if rec.user.ID != id || rec.user.OrganizationID != actor.OrganizationID {
			continue
		}
		if rec.user.Role == auth.RoleAdmin {
			return auth.ErrPermissionProtected
		}
		if m.revisions[id] != expected {
			return auth.ErrPermissionChanged
		}
		if rec.user.CanManageTraps == granted {
			return nil
		}
		if expected == math.MaxInt32 {
			return auth.ErrRevisionExhausted
		}
		rec.user.CanManageTraps = granted
		m.users[email] = rec
		m.revisions[id]++
		return nil
	}
	return auth.ErrNotFound
}

func (m *Store) TouchPresence(_ context.Context, id, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	now := time.Now().UTC()
	for _, sess := range m.Sessions {
		if sess.ID == id && sess.UserID == userID && sess.ExpiresAt.After(now) {
			if now.Sub(m.presence[id]) >= 30*time.Second {
				m.presence[id] = now
			}
			return nil
		}
	}
	return auth.ErrUnauthorized
}
