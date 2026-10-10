//go:build integration

package app

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	commandrepo "honey-forge/modules/commands/repository"
	commandservice "honey-forge/modules/commands/service"
	"honey-forge/modules/traps"
)

func TestTrapCommandLeaseRejectsRevokedSession(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "lease@example.test", auth.OrganizationInput{Mode: "create", Name: "Lease"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, _, _ := connectIntegrationAgent(t, r, a, trap)
	path := "/api/traps/" + trap.ID
	sendOperator(t, r, "POST", path+"/commands", `{"request_id":"`+string(contract.NewID())+`","action":"stop","params":{}}`, a, 201)
	agent := commandservice.NewAgent(commandrepo.New(r.pool, traps.NewRepository(r.pool)), CommandResultCheck(r.Catalog))
	dispatch, err := agent.Claim(ctx, identity.OrganizationID, trap.ID)
	if err != nil || dispatch == nil {
		t.Fatalf("claim: %+v %v", dispatch, err)
	}
	sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":1}`, a, 200)
	_, err = agent.ExtendLease(ctx, identity.OrganizationID, trap.ID, dispatch.CommandID, dispatch.LeaseID)
	var api *contract.Error
	if !errors.As(err, &api) || api.Code != "agent_unauthenticated" {
		t.Fatalf("revoked connection extended lease: %v", err)
	}
	var deadline time.Time
	if err := r.pool.QueryRow(t.Context(), `SELECT lease_expires_at FROM commands WHERE id=$1`, dispatch.CommandID).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	if !deadline.Equal(dispatch.LeaseExpiresAt) {
		t.Fatal("rejected progress changed the lease")
	}
}

func TestTrapCommandReplayReturnsInitialResponse(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "command-replay@example.test", auth.OrganizationInput{Mode: "create", Name: "Replay"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, _, runtime := connectIntegrationAgent(t, r, a, trap)
	command := executeIntegrationCommand(t, r, a, ctx, identity, "stop", runtime)
	path := "/api/traps/" + trap.ID + "/commands"
	// A different active command must not prevent replay of an earlier request.
	sendOperator(t, r, "POST", path, `{"request_id":"`+string(contract.NewID())+`","action":"stop","params":{}}`, a, 201)
	response := sendOperator(t, r, "POST", path, integrationJSON(t, commands.CreateRequest{RequestID: command.RequestID, Action: command.Action, Params: command.Params}), a, 200)
	replayed := decodeIntegration[commands.Command](t, response)
	if response.Header().Get("Idempotency-Replayed") != "true" || !reflect.DeepEqual(replayed, command) {
		t.Fatalf("replay must equal the initial queued response: got %+v, want %+v", replayed, command)
	}
	current := decodeIntegration[commands.Command](t, sendOperator(t, r, "GET", path+"/"+command.ID, "", a, 200))
	if current.Status != commands.Succeeded || current.FinishedAt == nil {
		t.Fatal("replay changed persisted outcome")
	}
}
