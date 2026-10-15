package repository

import (
	"context"
	"errors"
	"math"

	"github.com/jackc/pgx/v5"

	"honey-forge/modules/auth"
)

func (r *Repository) Members(ctx context.Context, actor auth.AuthContext, limit int, cursor string) (auth.MemberPage, error) {
	page := auth.MemberPage{Items: []auth.Member{}}
	var creatorID *string
	var confirmed bool
	err := r.pool.QueryRow(ctx, `SELECT o.creator_user_id::text, EXISTS (SELECT 1 FROM operator_sessions s
		WHERE s.id = $3 AND s.user_id = actor.id AND s.expires_at > now() AND s.admin_verified_until > now()) FROM organizations o
		JOIN users actor ON actor.organization_id = o.id
		WHERE o.id = $1 AND actor.id = $2 AND actor.role = 'admin'`, actor.OrganizationID, actor.UserID, actor.SessionID).Scan(&creatorID, &confirmed)
	if errors.Is(err, pgx.ErrNoRows) {
		return page, auth.ErrForbidden
	}
	if err != nil {
		return page, databaseError("checking member access", err)
	}
	if !confirmed {
		return page, auth.ErrAdminConfirmationRequired
	}
	if creatorID != nil {
		var creator auth.User
		err := r.pool.QueryRow(ctx, `SELECT id::text, organization_id::text, email, role, created_at,
			(role = 'admin' OR trap_management) FROM users WHERE organization_id = $1 AND id = $2`, actor.OrganizationID, *creatorID).
			Scan(&creator.ID, &creator.OrganizationID, &creator.Email, &creator.Role, &creator.CreatedAt, &creator.CanManageTraps)
		if err != nil {
			return page, databaseError("loading organization creator", err)
		}
		page.Creator = &creator
	}
	var after any
	if cursor != "" {
		after = cursor
	}
	rows, err := r.pool.Query(ctx, `SELECT u.id::text, u.organization_id::text, u.email, u.role, u.created_at,
		(u.role = 'admin' OR u.trap_management), u.permission_revision, p.last_seen_at,
		COALESCE(p.last_seen_at > now() - interval '90 seconds', false)
		FROM users u LEFT JOIN LATERAL (
			SELECT max(s.last_seen_at) AS last_seen_at FROM operator_sessions s
			WHERE s.user_id = u.id AND s.expires_at > now()
		) p ON true
		WHERE u.organization_id = $1 AND u.removed_at IS NULL AND ($2::uuid IS NULL OR u.id > $2::uuid)
		ORDER BY u.id LIMIT $3`, actor.OrganizationID, after, limit+1)
	if err != nil {
		return page, databaseError("listing members", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member auth.Member
		if err := rows.Scan(&member.ID, &member.OrganizationID, &member.Email, &member.Role, &member.CreatedAt,
			&member.CanManageTraps, &member.PermissionRevision, &member.LastSeenAt, &member.Online); err != nil {
			return page, databaseError("reading member", err)
		}
		page.Items = append(page.Items, member)
	}
	if err := rows.Err(); err != nil {
		return page, databaseError("listing members", err)
	}
	if len(page.Items) > limit {
		next := page.Items[limit-1].ID
		page.NextCursor = &next
		page.Items = page.Items[:limit]
	}
	return page, nil
}

func (r *Repository) SetTrapPermission(ctx context.Context, actor auth.AuthContext, id string, granted bool, expected int32) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var allowed bool
		var confirmed bool
		err := tx.QueryRow(ctx, `SELECT COALESCE(o.creator_user_id = actor.id, false) AND actor.role = 'admin',
			EXISTS (SELECT 1 FROM operator_sessions s WHERE s.id = $3 AND s.user_id = actor.id
			AND s.expires_at > now() AND s.admin_verified_until > now())
			FROM organizations o JOIN users actor ON actor.organization_id = o.id
			WHERE o.id = $1 AND actor.id = $2 FOR NO KEY UPDATE OF o`, actor.OrganizationID, actor.UserID, actor.SessionID).Scan(&allowed, &confirmed)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
			return auth.ErrForbidden
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return auth.ErrAdminConfirmationRequired
		}
		var role string
		var current bool
		var revision int32
		err = tx.QueryRow(ctx, `SELECT role, trap_management, permission_revision FROM users
			WHERE organization_id = $1 AND id = $2 AND removed_at IS NULL FOR NO KEY UPDATE`, actor.OrganizationID, id).Scan(&role, &current, &revision)
		if err != nil {
			return err
		}
		if role == auth.RoleAdmin {
			return auth.ErrPermissionProtected
		}
		if revision != expected {
			return auth.ErrPermissionChanged
		}
		if current == granted {
			return nil
		}
		if revision == math.MaxInt32 {
			return auth.ErrRevisionExhausted
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET trap_management = $1, permission_revision = permission_revision + 1
			WHERE organization_id = $2 AND id = $3`, granted, actor.OrganizationID, id); err != nil {
			return err
		}
		action := "member.trap_management_revoked"
		if granted {
			action = "member.trap_management_granted"
		}
		return appendAudit(ctx, tx, actor.UserID, action, "user", id)
	})
	return databaseError("setting trap permission and audit", err)
}

func (r *Repository) TouchPresence(ctx context.Context, id, userID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE operator_sessions SET last_seen_at = now()
		WHERE id = $1 AND user_id = $2 AND expires_at > now()
		AND (last_seen_at IS NULL OR last_seen_at <= now() - interval '30 seconds')`, id, userID)
	if err != nil {
		return databaseError("updating presence", err)
	}
	var active bool
	err = r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM operator_sessions
		WHERE id = $1 AND user_id = $2 AND expires_at > now())`, id, userID).Scan(&active)
	if err != nil {
		return databaseError("checking presence session", err)
	}
	if !active {
		return auth.ErrUnauthorized
	}
	return nil
}
