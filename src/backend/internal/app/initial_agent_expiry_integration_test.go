//go:build integration

package app

import (
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	commandrepo "honey-forge/modules/commands/repository"
	"honey-forge/modules/traps"
)

func TestNeverConnectedAgentQueueRecoversAutomatically(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "initial-agent@example.test", auth.OrganizationInput{Mode: "create", Name: "Initial agent"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	path := "/api/traps/" + trap.ID
	command := decodeIntegration[commands.Command](t, sendOperator(t, r, "POST", path+"/commands", `{"request_id":"`+string(contract.NewID())+`","action":"apply_config","params":{"profile_revision":1}}`, a, 201))
	before := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	repo := commandrepo.New(r.pool, traps.NewRepository(r.pool))
	now := command.CreatedAt.Add(5 * time.Minute)
	if err := repo.ExpireAllDue(t.Context(), now.Add(-time.Microsecond)); err != nil {
		t.Fatal(err)
	}

	stillQueued := decodeIntegration[commands.Command](t, sendOperator(t, r, "GET", path+"/commands/"+command.ID, "", a, 200))
	if stillQueued.Status != commands.Queued {
		t.Fatal("initial connection deadline expired early")
	}

	for range 2 {
		if err := repo.ExpireAllDue(t.Context(), now); err != nil {
			t.Fatal(err)
		}
	}

	expired := decodeIntegration[commands.Command](t, sendOperator(t, r, "GET", path+"/commands/"+command.ID, "", a, 200))
	after := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	if expired.Status != commands.Expired || expired.Error == nil || expired.Error.Code != "agent_not_connected" || expired.FinishedAt == nil || !expired.FinishedAt.Equal(now) {
		t.Fatalf("command not recovered: %+v", expired)
	}

	if after.ActiveCommandID != nil || after.StateVersion != before.StateVersion+1 || after.Revision != before.Revision || after.RuntimeState != "unknown" || after.AppliedProfileRevision != nil {
		t.Fatalf("recovery invented runtime or did not release command: %+v", after)
	}

	trapMutation(t, r, a, "DELETE", path, "", 1, 204)
}
