package traps

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
	"honey-forge/modules/profiles"
)

// Serialize reservations across organizations. Unpinned legacy traps reserve
// ports on every node because their actual placement is not known.
func checkManagedPorts(ctx context.Context, tx pgx.Tx, configObject profiles.Object, excludeTrapID, nodeID string) error {
	const lockID int64 = 0x6866706f7274
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
		return fmt.Errorf("lock managed trap ports: %w", err)
	}

	config, err := json.Marshal(configObject)
	if err != nil {
		return fmt.Errorf("encode managed trap ports: %w", err)
	}

	var conflict bool
	err = tx.QueryRow(ctx, `
		WITH occupied AS (
			SELECT COALESCE(t.applied_configuration->'config', r.config::jsonb) AS config
			FROM traps t JOIN profiles p ON p.id=t.profile_id
			JOIN profile_revisions r ON r.profile_id=p.id AND r.revision=p.revision
			WHERE t.auto_deploy AND t.deleted_at IS NULL AND t.id::text <> $2 AND ($3='' OR t.deployment_node_id='' OR t.deployment_node_id=$3)
			UNION ALL
			SELECT c.configuration->'config' FROM traps t JOIN commands c ON c.id=t.active_command_id
			WHERE t.auto_deploy AND t.deleted_at IS NULL AND t.id::text <> $2 AND ($3='' OR t.deployment_node_id='' OR t.deployment_node_id=$3) AND c.action='apply_config' AND c.status IN ('queued','running')
		)
		SELECT EXISTS (
			SELECT 1 FROM occupied o,
			jsonb_array_elements(COALESCE(o.config->'listeners','[]'::jsonb) || COALESCE(o.config->'services','[]'::jsonb)) existing,
			jsonb_array_elements(COALESCE($1::jsonb->'listeners','[]'::jsonb) || COALESCE($1::jsonb->'services','[]'::jsonb)) requested
			WHERE (existing->>'port')::numeric = (requested->>'port')::numeric
		)`, string(config), excludeTrapID, nodeID).Scan(&conflict)
	if err != nil {
		return fmt.Errorf("check managed trap ports: %w", err)
	}
	if conflict {
		return contract.NewError("trap_port_conflict")
	}

	return nil
}
