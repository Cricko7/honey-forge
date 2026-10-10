package traps

import (
	"context"
	"fmt"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
)

// ExpireConnections runs independently of REST reads. It fences a stale
// connection under the trap lock, so a concurrent accepted heartbeat wins.
func (g *Gateway) ExpireConnections(ctx context.Context, now time.Time) error {
	rows, err := g.repository.pool.Query(ctx, `SELECT organization_id::text,id::text,connection_id::text FROM traps WHERE deleted_at IS NULL AND connection_id IS NOT NULL AND last_seen_at<$1`, now.Add(-30*time.Second))
	if err != nil {
		return fmt.Errorf("find stale connections: %w", err)
	}
	type stale struct {
		identity   agentws.Identity
		connection string
	}
	var items []stale
	for rows.Next() {
		var item stale
		if err := rows.Scan(&item.identity.OrganizationID, &item.identity.TrapID, &item.connection); err != nil {
			rows.Close()
			return fmt.Errorf("scan stale connection: %w", err)
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("iterate stale connections: %w", err)
	}
	for _, item := range items {
		agentCtx := contract.WithPrincipal(ctx, contract.Principal{OrganizationID: contract.ID(item.identity.OrganizationID), TrapID: contract.ID(item.identity.TrapID), Role: contract.Agent})
		if err := g.expireConnection(agentCtx, item.identity, item.connection, now); err != nil {
			return err
		}
	}
	return nil
}
