//go:build integration

package app

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	commandrepo "honey-forge/modules/commands/repository"
	commandservice "honey-forge/modules/commands/service"
	"honey-forge/modules/traps"
)

func TestTrapCommandOfflineExpiryReleasesRegistration(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "offline-expiry@example.test", auth.OrganizationInput{Mode: "create", Name: "Expiry"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	path := "/api/traps/" + trap.ID
	command := decodeIntegration[commands.Command](t, sendOperator(t, r, "POST", path+"/commands", `{"request_id":"`+string(contract.NewID())+`","action":"stop","params":{}}`, a, 201))
	before := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	requireOperatorError(t, trapMutation(t, r, a, "DELETE", path, "", 1, 409), "command_in_progress")
	repo := commandrepo.New(r.pool, traps.NewRepository(r.pool))
	if err := repo.ExpireAllDue(t.Context(), command.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	expired := decodeIntegration[commands.Command](t, sendOperator(t, r, "GET", path+"/commands/"+command.ID, "", a, 200))
	after := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	if expired.Status != commands.Expired || expired.Error == nil || expired.Error.Code != "command_expired" || expired.FinishedAt == nil {
		t.Fatal("scheduler did not persist expiration")
	}
	if after.ActiveCommandID != nil || after.StateVersion != before.StateVersion+1 || after.Revision != 1 || after.RuntimeState != "unknown" || after.Connectivity != "offline" {
		t.Fatal("expiry did not release the command or invented runtime state")
	}
	trapMutation(t, r, a, "DELETE", path, "", 1, 204)
	sendOperator(t, r, "GET", path+"/commands/"+command.ID, "", a, 200)
}

func TestTrapCommandCompletionRollsBackOnStateExhaustion(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "completion-overflow@example.test", auth.OrganizationInput{Mode: "create", Name: "Overflow"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, _, runtime := connectIntegrationAgent(t, r, a, trap)
	path := "/api/traps/" + trap.ID
	command := decodeIntegration[commands.Command](t, sendOperator(t, r, "POST", path+"/commands", `{"request_id":"`+string(contract.NewID())+`","action":"apply_config","params":{"profile_revision":1}}`, a, 201))
	agent := commandservice.NewAgent(commandrepo.New(r.pool, traps.NewRepository(r.pool)), CommandResultCheck(r.Catalog))
	dispatch, err := agent.Claim(ctx, identity.OrganizationID, trap.ID)
	if err != nil || dispatch == nil {
		t.Fatalf("claim: %+v %v", dispatch, err)
	}
	if _, err := r.pool.Exec(t.Context(), `UPDATE traps SET state_version=2147483647 WHERE id=$1`, trap.ID); err != nil {
		t.Fatal(err)
	}
	var revision int32 = 1
	runtime.AppliedProfileRevision = &revision
	input := commands.AgentResult{CommandID: command.ID, LeaseID: dispatch.LeaseID, Status: commands.Succeeded, Result: json.RawMessage(`{"runtime_state":"stopped","applied_profile_revision":1}`), Runtime: runtime}
	_, err = agent.RecordResult(ctx, identity.OrganizationID, trap.ID, input)
	var api *contract.Error
	if !errors.As(err, &api) || api.Code != "revision_exhausted" {
		t.Fatalf("exhausted completion: %v", err)
	}
	stored := decodeIntegration[commands.Command](t, sendOperator(t, r, "GET", path+"/commands/"+command.ID, "", a, 200))
	current := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	if stored.Status != commands.Running || stored.FinishedAt != nil || current.ActiveCommandID == nil || *current.ActiveCommandID != command.ID || current.AppliedProfileRevision != nil {
		t.Fatal("rejected completion partially changed command/trap")
	}
	var snapshots, terminalChanges int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM traps WHERE id=$1 AND applied_configuration IS NOT NULL`, trap.ID).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM mutation_changes WHERE resource_id=$1 AND type='command.changed'`, command.ID).Scan(&terminalChanges); err != nil {
		t.Fatal(err)
	}
	if snapshots != 0 || terminalChanges != 2 { // create + claim; no terminal notification
		t.Fatal("rejected completion published changes or installed a snapshot")
	}
}
