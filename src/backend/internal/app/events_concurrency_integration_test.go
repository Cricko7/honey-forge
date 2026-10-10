//go:build integration

package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/auth"
	"honey-forge/modules/events"
	"honey-forge/modules/traps"
)

func TestGlobalEventIDConflict(t *testing.T) {
	r := openIntegrationRuntime(t)
	var sessions [2]operatorSession
	var contexts [2]context.Context
	var identities [2]agentws.Identity
	var connections [2]string
	for i, email := range []string{"global-one@example.test", "global-two@example.test"} {
		sessions[i] = registerOperator(t, r, email, auth.OrganizationInput{Mode: "create", Name: "Global"})
		_, trap, _ := createIntegrationTrap(t, r, sessions[i])
		ctx, identity, connection, runtime := connectIntegrationAgent(t, r, sessions[i], trap)
		revision := int32(1)
		runtime.AppliedProfileRevision = &revision
		executeIntegrationCommand(t, r, sessions[i], ctx, identity, "apply_config", runtime)
		contexts[i], identities[i], connections[i] = ctx, identity, connection
	}
	event := attackEvent(time.Now().UTC())
	batch := eventBatch(t, event)
	var results [2]error
	ready := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() { <-ready; _, results[i] = r.Agents.Ingest(contexts[i], identities[i], connections[i], batch) })
	}
	close(ready)
	wg.Wait()
	winner, loser := -1, -1
	for i, err := range results {
		if err == nil {
			winner = i
		} else {
			var api *contract.Error
			if !errors.As(err, &api) || api.Code != "event_id_conflict" {
				t.Fatal(err)
			}
			loser = i
		}
	}
	if winner < 0 || loser < 0 {
		t.Fatal("globally reused event_id accepted by both traps")
	}
	detail := decodeIntegration[events.Event](t, sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", sessions[winner], 200))
	if detail.TrapID != identities[winner].TrapID {
		t.Fatal("owner changed")
	}
	sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", sessions[loser], 404)
}

func TestEventReplayAfterCredentialRotation(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "event-rotation@example.test", auth.OrganizationInput{Mode: "create", Name: "Rotation"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	event := attackEvent(time.Now().UTC())
	batch := eventBatch(t, event)
	first, err := r.Agents.Ingest(ctx, identity, connection, batch)
	if err != nil {
		t.Fatal(err)
	}
	credentials := decodeIntegration[traps.AgentCredentials](t, sendOperator(t, r, "POST", "/api/traps/"+trap.ID+"/agent-credentials", `{"expected_generation":1}`, a, 200))
	newIdentity, err := r.Agents.Authenticate(t.Context(), credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	newConnection := string(contract.NewID())
	newContext := agentws.WithSession(ctx, newIdentity, newConnection)
	if _, err := r.Agents.Hello(newContext, newIdentity, newConnection, agentws.AgentHello{AgentVersion: "0.1", Hostname: "decoy", Runtime: runtime}); err != nil {
		t.Fatal(err)
	}
	second, err := r.Agents.Ingest(newContext, newIdentity, newConnection, batch)
	if err != nil || !second.StoredAt.Equal(first.StoredAt) {
		t.Fatalf("replayed timestamp changed: err=%v", err)
	}
	if _, err := r.Agents.Ingest(ctx, identity, connection, batch); err == nil {
		t.Fatal("revoked identity accepted")
	}
}
