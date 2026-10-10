//go:build integration

package app

import (
	"context"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/auth"
	"honey-forge/modules/traps"
)

type cancelledRejection struct{ cancel context.CancelFunc }

func (f cancelledRejection) Ingest(context.Context, agentws.Identity, string, agentws.TelemetryBatch) (agentws.TelemetryAck, error) {
	f.cancel()
	return agentws.TelemetryAck{}, contract.NewError("telemetry_invalid")
}

func TestEventRejectedBatchReleasesFenceAfterCancellation(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "rejected@example.test", auth.OrganizationInput{Mode: "create", Name: "Rejected"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, connection, _ := connectIntegrationAgent(t, r, a, trap)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	gateway := traps.NewGateway(traps.NewRepository(r.pool), r.Catalog, cancelledRejection{cancel})
	_, err := gateway.Ingest(ctx, identity, connection, eventBatch(t, attackEvent(time.Now().UTC())))
	assertEventError(t, err, "telemetry_invalid")
	var pending int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM trap_ingestions WHERE trap_id=$1 AND NOT finished`, trap.ID).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("rejected batch still blocks deletion: pending=%d err=%v", pending, err)
	}
}
