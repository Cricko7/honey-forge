package repository

import (
	"context"
	"fmt"
	profilecore "github.com/Cricko7/honey-forge/src/backend/modules/profiles"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) List(ctx context.Context, org string, q profilecore.ListQuery) ([]profilecore.Profile, int64, error) {
	items := []profilecore.Profile{}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if q.Boundary == 0 {
			var id string
			if err := tx.QueryRow(ctx, `SELECT id::text FROM organizations WHERE id=$1 FOR SHARE`, org).Scan(&id); err != nil {
				return err
			}

			if err := tx.QueryRow(ctx, `SELECT coalesce(max(sequence),0) FROM profiles WHERE organization_id=$1`, org).Scan(&q.Boundary); err != nil {
				return err
			}
		}

		rows, err := tx.Query(ctx, `SELECT `+profileColumns+` FROM profiles p JOIN profile_revisions r ON r.profile_id=p.id AND r.revision=p.revision
			WHERE p.organization_id=$1 AND p.deleted_at IS NULL AND p.sequence<=$2 AND ($3='' OR p.type_id=$3)
			AND ($4='' OR (p.created_at,p.id)<($5::timestamptz,NULLIF($4,'')::uuid)) ORDER BY p.created_at DESC,p.id DESC LIMIT $6`, org, q.Boundary, q.TypeID, q.AfterID, q.AfterTime, q.Limit+1)
		if err != nil {
			return fmt.Errorf("listing profiles: %w", err)
		}

		defer rows.Close()
		for rows.Next() {
			p, err := scanProfile(rows)
			if err != nil {
				return err
			}

			items = append(items, p)
		}

		return rows.Err()
	})
	return items, q.Boundary, databaseError("listing profiles transaction", err)
}
