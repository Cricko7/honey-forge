package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"honey-forge/modules/auth"
	profilecore "honey-forge/modules/profiles"
)

// DeleteUnbound runs in the trap deletion transaction, which already holds the
// organization journal lock. The profile lock fences new bindings until commit.
func (r *Repository) DeleteUnbound(ctx context.Context, tx pgx.Tx, a auth.AuthContext, id string) error {
	p, err := lockedProfile(ctx, tx, a.OrganizationID, id)
	if errors.Is(err, profilecore.ErrNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("lock unbound profile: %w", err)
	}

	bound, err := CheckLiveBindings(ctx, a.OrganizationID, id, tx)
	if err != nil {
		return fmt.Errorf("check profile bindings: %w", err)
	}

	if bound {
		return nil
	}

	return r.deleteProfile(ctx, tx, a, p)
}

func (r *Repository) deleteProfile(ctx context.Context, tx pgx.Tx, a auth.AuthContext, p profilecore.Profile) error {
	if _, err := tx.Exec(ctx, `UPDATE profiles SET deleted_at=clock_timestamp() WHERE organization_id=$1 AND id=$2`, a.OrganizationID, p.ID); err != nil {
		return fmt.Errorf("tombstone profile: %w", err)
	}

	return r.recordMutation(ctx, tx, a, "profile.deleted", p, []string{})
}
