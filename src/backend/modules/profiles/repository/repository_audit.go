package repository

import (
	"context"
	"encoding/json"
	"fmt"
	profilecore "honey-forge/modules/profiles"

	"github.com/jackc/pgx/v5"
	"honey-forge/modules/auth"
)

type Writers struct {
	AppendAudit  func(context.Context, pgx.Tx, auth.AuthContext, string, string, int32, []string) (string, error)
	AppendChange func(context.Context, pgx.Tx, string, string, profilecore.Object) error
}

func (r *Repository) recordMutation(ctx context.Context, tx pgx.Tx, a auth.AuthContext, action string, p profilecore.Profile, fields []string) error {
	if r.writers.AppendAudit == nil || r.writers.AppendChange == nil {
		return profilecore.ErrUnavailable
	}

	id, err := r.writers.AppendAudit(ctx, tx, a, action, p.ID, p.Revision, fields)
	if err != nil {
		return fmt.Errorf("recording profile audit: %w", err)
	}

	event := "profile.changed"
	data := profilecore.Object{"profile_id": p.ID, "revision": p.Revision}
	if action == "profile.deleted" {
		event = "profile.deleted"
		delete(data, "revision")
	}

	if err := r.writers.AppendChange(ctx, tx, a.OrganizationID, event, data); err != nil {
		return fmt.Errorf("recording profile notification: %w", err)
	}

	if err := r.writers.AppendChange(ctx, tx, a.OrganizationID, "audit.created", profilecore.Object{"audit_id": id}); err != nil {
		return fmt.Errorf("recording audit notification: %w", err)
	}

	return nil
}

func appendAudit(ctx context.Context, tx pgx.Tx, a auth.AuthContext, action, profileID string, revision int32, fields []string) (string, error) {
	id := profilecore.NewID()
	raw, err := json.Marshal(profilecore.Object{"profile_revision": revision, "changed_fields": fields})
	if err != nil {
		return "", err
	}

	tag, err := tx.Exec(ctx, `INSERT INTO profile_audit(id,organization_id,actor_user_id,actor_email,actor_role,action,resource_id,details)
		SELECT $1,organization_id,id,email,role,$4,$5,$6::jsonb FROM users WHERE id=$2 AND organization_id=$3`, id, a.UserID, a.OrganizationID, action, profileID, string(raw))
	if err != nil {
		return "", err
	}

	if tag.RowsAffected() != 1 {
		return "", profilecore.ErrUnauthorized
	}

	return id, nil
}

func appendChange(ctx context.Context, tx pgx.Tx, org, event string, data profilecore.Object) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `INSERT INTO profile_changes(organization_id,type,data) VALUES($1,$2,$3::jsonb)`, org, event, string(raw))
	return err
}
