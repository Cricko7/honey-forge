package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"honey-forge/internal/contract"
	"honey-forge/modules/profiles"
)

func (r *Repository) checkPayload(ctx context.Context, tx pgx.Tx, trapID string, event AgentEvent, snapshot profiles.Snapshot, cumulative bool) error {
	if event.TypeID != "tcp-banner" || event.TypeVersion != 1 || event.EventType != "tcp.payload_received" {
		return nil
	}
	config, err := json.Marshal(snapshot.Config)
	if err != nil {
		return fmt.Errorf("encode payload snapshot: %w", err)
	}
	var captured int64
	if cumulative {
		// The caller holds the trap lock; retries were deduplicated before this sum.
		err := tx.QueryRow(ctx, `SELECT COALESCE(sum((event_data->>'captured_bytes')::bigint),0)::bigint FROM trap_events WHERE trap_id=$1 AND session_id=$2 AND envelope->>'event_type'='tcp.payload_received'`, trapID, event.SessionID).Scan(&captured)
		if err != nil {
			return fmt.Errorf("read session capture budget: %w", err)
		}
	}
	_, err = r.catalog.CheckPayloadCapture(ctx, event.TypeID, contract.TypeVersion(event.TypeVersion), config, event.Data, captured)
	return err
}
