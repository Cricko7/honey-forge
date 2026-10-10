package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	profilecore "honey-forge/modules/profiles"

	"github.com/jackc/pgx/v5"
)

func insertRevision(ctx context.Context, tx pgx.Tx, p profilecore.Profile) error {
	config, err := json.Marshal(p.Config)
	if err != nil {
		return fmt.Errorf("encoding snapshot config: %w", err)
	}

	secrets, err := json.Marshal(p.SecretFieldsSet)
	if err != nil {
		return fmt.Errorf("encoding snapshot secret paths: %w", err)
	}

	_, err = tx.Exec(ctx, `INSERT INTO profile_revisions(profile_id,revision,name,description,config,secret_fields_set,updated_at) VALUES($1,$2,$3,$4,$5::json,$6::jsonb,$7)`, p.ID, p.Revision, p.Name, p.Description, string(config), string(secrets), p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("inserting profile revision: %w", err)
	}

	return nil
}

func readRevision(ctx context.Context, tx pgx.Tx, org, id string, revision int32) (profilecore.Profile, error) {
	return scanProfile(tx.QueryRow(ctx, `SELECT `+profileColumns+` FROM profiles p JOIN profile_revisions r ON r.profile_id=p.id WHERE p.organization_id=$1 AND p.id=$2 AND r.revision=$3`, org, id, revision))
}

func (r *Repository) ReadSnapshot(ctx context.Context, org, id string, revision int32) (profilecore.Snapshot, error) {
	p, err := scanProfile(r.pool.QueryRow(ctx, `SELECT `+profileColumns+` FROM profiles p JOIN profile_revisions r ON r.profile_id=p.id WHERE p.organization_id=$1 AND p.id=$2 AND r.revision=$3`, org, id, revision))
	if err != nil {
		return profilecore.Snapshot{}, databaseError("reading immutable snapshot", err)
	}

	return profilecore.SnapshotOf(p), nil
}

// CaptureProfile runs inside the command transaction and fences PATCH/DELETE until commit.
func CaptureProfile(ctx context.Context, tx pgx.Tx, org, id string, revision int32) (profilecore.Snapshot, error) {
	p, err := ReadProfileForBinding(ctx, tx, org, id)
	if err != nil {
		return profilecore.Snapshot{}, err
	}

	if p.Revision != revision {
		return profilecore.Snapshot{}, profilecore.ErrProfileChanged
	}

	return profilecore.SnapshotOf(p), nil
}

// ReadProfileForBinding must be used by trap creation in the transaction inserting its binding.
// The returned full config is internal data and must never be sent to an operator.
func ReadProfileForBinding(ctx context.Context, tx pgx.Tx, org, id string) (profilecore.Profile, error) {
	if tx == nil {
		return profilecore.Profile{}, profilecore.ErrUnavailable
	}

	var revision int32
	err := tx.QueryRow(ctx, `SELECT revision FROM profiles WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`, org, id).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return profilecore.Profile{}, profilecore.ErrNotFound
	}

	if err != nil {
		return profilecore.Profile{}, databaseError("locking profile for binding", err)
	}

	p, err := readRevision(ctx, tx, org, id, revision)
	return p, databaseError("locking profile for binding", err)
}

// CheckLiveBindings uses the shared profile lock held by Delete.
func CheckLiveBindings(ctx context.Context, org, id string, tx pgx.Tx) (bool, error) {
	if tx == nil {
		return false, profilecore.ErrUnavailable
	}

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('traps') IS NOT NULL`).Scan(&exists); err != nil {
		return false, databaseError("checking trap storage", err)
	}

	if !exists {
		return false, nil
	}

	// Module 05's SQL adapter must preserve these columns or supply its own function.
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM traps WHERE organization_id=$1 AND profile_id=$2 AND deleted_at IS NULL)`, org, id).Scan(&exists)
	return exists, databaseError("checking live profile bindings", err)
}
