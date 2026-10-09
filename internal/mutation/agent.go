package mutation

import (
	"context"
	"fmt"
	"honey-forge/internal/contract"

	"github.com/jackc/pgx/v5"
)

// AgentWrite fixes events/runtime observations and their notifications atomically.
// It uses the pinned identity; it does not advance the editable resource revision.
// Operator business actions requiring audit use Create/Write/Delete instead.
func (s *Store) AgentWrite(ctx context.Context, trapID contract.ID, write func(context.Context, pgx.Tx) ([]Change, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := contract.RequireAgentTrap(ctx, trapID); err != nil {
		return err
	}
	principal, _ := contract.PrincipalFrom(ctx)
	return s.transaction(ctx, principal.OrganizationID, func(tx pgx.Tx) error {
		changes, err := write(ctx, tx)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			return fmt.Errorf("agent changes require notification")
		}
		for _, change := range changes {
			if !contract.ValidTypeID(change.Type) || !contract.ValidID(string(change.ResourceID)) {
				return fmt.Errorf("invalid agent change")
			}
		}
		_, err = recordChanges(ctx, tx, principal.OrganizationID, changes)
		return err
	})
}
