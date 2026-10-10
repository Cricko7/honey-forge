package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	profilecore "honey-forge/modules/profiles"

	"github.com/jackc/pgx/v5"
	"honey-forge/modules/auth"
)

func (r *Repository) Create(ctx context.Context, a auth.AuthContext, key string, hash [32]byte, build func() (profilecore.Profile, error)) (profilecore.CreateResult, error) {
	var result profilecore.CreateResult
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, a.OrganizationID); err != nil {
			return err
		}
		// This also fences first-page boundaries against uncommitted creations.
		var org string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM organizations WHERE id=$1 FOR UPDATE`, a.OrganizationID).Scan(&org); err != nil {
			return err
		}

		var oldHash []byte
		var id string
		var deleted bool
		var revision int32
		err := tx.QueryRow(ctx, `SELECT k.request_hash,k.profile_id::text,p.deleted_at IS NOT NULL,p.revision FROM profile_requests k JOIN profiles p ON p.id=k.profile_id WHERE k.organization_id=$1 AND k.request_id=$2`, org, key).Scan(&oldHash, &id, &deleted, &revision)
		if err == nil {
			if !bytes.Equal(hash[:], oldHash) {
				return profilecore.ErrIdempotencyConflict
			}

			if deleted {
				return profilecore.ErrRequestUsed
			}

			p, err := readRevision(ctx, tx, org, id, 1)
			if err != nil {
				return err
			}

			result = profilecore.CreateResult{Profile: p, Replayed: true, Current: revision == 1}
			return nil
		}

		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		p, err := build()
		if err != nil {
			return err
		}

		err = tx.QueryRow(ctx, `INSERT INTO profiles(id,organization_id,type_id,type_version,interaction_level,revision,created_at) VALUES($1,$2,$3,$4,$5,1,$6) RETURNING sequence`, p.ID, org, p.TypeID, p.TypeVersion, p.InteractionLevel, p.CreatedAt).Scan(&p.Sequence)
		if err != nil {
			return fmt.Errorf("inserting profile: %w", err)
		}

		if err := insertRevision(ctx, tx, p); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `INSERT INTO profile_requests(organization_id,request_id,request_hash,profile_id) VALUES($1,$2,$3,$4)`, org, key, hash[:], p.ID); err != nil {
			return err
		}

		if err := r.recordMutation(ctx, tx, a, "profile.created", p, []string{}); err != nil {
			return err
		}

		result = profilecore.CreateResult{Profile: p, Current: true}
		return nil
	})
	return result, databaseError("creating profile transaction", err)
}

func (r *Repository) Update(ctx context.Context, a auth.AuthContext, id string, update func(profilecore.Profile) (profilecore.Profile, []string, error)) (profilecore.Profile, error) {
	var result profilecore.Profile
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, a.OrganizationID); err != nil {
			return err
		}
		p, err := lockedProfile(ctx, tx, a.OrganizationID, id)
		if err != nil {
			return err
		}

		p, fields, err := update(p)
		if err != nil {
			return err
		}

		result = p
		if len(fields) == 0 {
			return nil
		}

		if err := insertRevision(ctx, tx, p); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE profiles SET revision=$3 WHERE organization_id=$1 AND id=$2`, a.OrganizationID, id, p.Revision); err != nil {
			return err
		}

		return r.recordMutation(ctx, tx, a, "profile.updated", p, fields)
	})
	return result, databaseError("updating profile transaction", err)
}

func (r *Repository) Delete(ctx context.Context, a auth.AuthContext, id string, check func(profilecore.Profile, pgx.Tx) error) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, a.OrganizationID); err != nil {
			return err
		}
		p, err := lockedProfile(ctx, tx, a.OrganizationID, id)
		if err != nil {
			return err
		}

		if err := check(p, tx); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE profiles SET deleted_at=clock_timestamp() WHERE organization_id=$1 AND id=$2`, a.OrganizationID, id); err != nil {
			return err
		}

		return r.recordMutation(ctx, tx, a, "profile.deleted", p, []string{})
	})
	return databaseError("deleting profile transaction", err)
}

// Lock before profiles/organizations, matching trap/command transaction order.
func lockStream(ctx context.Context, tx pgx.Tx, org string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO organization_changes(organization_id) VALUES($1) ON CONFLICT DO NOTHING`, org); err != nil {
		return fmt.Errorf("initialize profile stream: %w", err)
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT sequence FROM organization_changes WHERE organization_id=$1 FOR UPDATE`, org).Scan(&sequence); err != nil {
		return fmt.Errorf("lock profile stream: %w", err)
	}
	return nil
}
