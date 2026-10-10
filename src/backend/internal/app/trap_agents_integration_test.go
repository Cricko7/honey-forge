//go:build integration

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	commandrepo "honey-forge/modules/commands/repository"
	commandservice "honey-forge/modules/commands/service"
	"honey-forge/modules/events"
	"honey-forge/modules/traps"
)

func connectIntegrationAgent(t *testing.T, r *Runtime, a operatorSession, trap traps.Trap) (context.Context, agentws.Identity, string, commands.AgentRuntime) {
	t.Helper()
	credentials := decodeIntegration[traps.AgentCredentials](t, sendOperator(t, r, "POST", "/api/traps/"+trap.ID+"/agent-credentials", `{"expected_generation":0}`, a, 200))
	identity, err := r.Agents.Authenticate(t.Context(), credentials.Token)
	if err != nil {
		t.Fatal(err)
	}
	connection := string(contract.NewID())
	ctx := contract.WithPrincipal(t.Context(), contract.Principal{OrganizationID: contract.ID(identity.OrganizationID), TrapID: contract.ID(trap.ID), Role: contract.Agent})
	ctx = agentws.WithSession(ctx, identity, connection)
	runtime := commands.AgentRuntime{RuntimeState: "stopped", BufferState: "ok", BufferCapacityBytes: 1024}
	_, err = r.Agents.Hello(ctx, identity, connection, agentws.AgentHello{AgentVersion: "0.1", Hostname: "decoy", Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, identity, connection, runtime
}
func executeIntegrationCommand(t *testing.T, r *Runtime, a operatorSession, ctx context.Context, identity agentws.Identity, action string, runtime commands.AgentRuntime) commands.Command {
	t.Helper()
	params := json.RawMessage(`{}`)
	if action == "apply_config" {
		params = json.RawMessage(`{"profile_revision":1}`)
	}
	command := decodeIntegration[commands.Command](t, sendOperator(t, r, "POST", "/api/traps/"+identity.TrapID+"/commands", integrationJSON(t, commands.CreateRequest{RequestID: string(contract.NewID()), Action: action, Params: params}), a, 201))
	repo := commandrepo.New(r.pool, traps.NewRepository(r.pool))
	agent := commandservice.NewAgent(repo, CommandResultCheck(r.Catalog))
	dispatch, err := agent.Claim(ctx, identity.OrganizationID, identity.TrapID)
	if err != nil || dispatch == nil {
		t.Fatalf("dispatch=%+v err=%v", dispatch, err)
	}
	result := json.RawMessage(fmt.Sprintf(`{"runtime_state":%q,"applied_profile_revision":null}`, runtime.RuntimeState))
	if runtime.AppliedProfileRevision != nil {
		result = json.RawMessage(fmt.Sprintf(`{"runtime_state":%q,"applied_profile_revision":%d}`, runtime.RuntimeState, *runtime.AppliedProfileRevision))
	}
	_, err = agent.RecordResult(ctx, identity.OrganizationID, identity.TrapID, commands.AgentResult{CommandID: command.ID, LeaseID: dispatch.LeaseID, Status: commands.Succeeded, Result: result, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	return command
}
func TestTrapHeartbeatAndSafeDeletion(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "heartbeat@example.test", auth.OrganizationInput{Mode: "create", Name: "Heartbeat"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	path := "/api/traps/" + trap.ID
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	before := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	if before.Connectivity != "online" || before.Agent.Hostname != "decoy" || before.Revision != 1 || !before.UpdatedAt.Equal(trap.UpdatedAt) {
		t.Fatal(before)
	}
	patched := decodeIntegration[traps.Trap](t, trapMutation(t, r, a, "PATCH", path, `{"description":"edited"}`, 1, 200))
	if _, err := r.Agents.Observe(ctx, identity, connection, runtime); err != nil {
		t.Fatal(err)
	}
	after := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	if after.Revision != 2 || after.StateVersion <= patched.StateVersion || !after.UpdatedAt.Equal(patched.UpdatedAt) {
		t.Fatal(after)
	}
	runtime.BufferedEvents = 1
	if _, err := r.Agents.Observe(ctx, identity, connection, runtime); err != nil {
		t.Fatal(err)
	}
	trapMutation(t, r, a, "DELETE", path, "", 2, 409)
	runtime.BufferedEvents = 0
	if _, err := r.Agents.Observe(ctx, identity, connection, runtime); err != nil {
		t.Fatal(err)
	}
	batch := string(contract.NewID())
	repo := traps.NewRepository(r.pool)
	if err := repo.BeginIngestion(ctx, identity, connection, batch); err != nil {
		t.Fatal(err)
	}
	trapMutation(t, r, a, "DELETE", path, "", 2, 409)
	// Token replacement must not discard a pending reservation.
	sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":1}`, a, 200)
	var pending int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM trap_ingestions WHERE trap_id=$1 AND NOT finished`, trap.ID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	if _, err := r.Agents.Observe(ctx, identity, connection, runtime); err == nil {
		t.Fatal("revoked identity accepted")
	}
	if err := repo.FinishIngestion(ctx, identity, batch); err != nil {
		t.Fatal(err)
	}
	trapMutation(t, r, a, "DELETE", path, "", 2, 409) // offline is never inferred stopped
}
func TestTrapCommandsEventsAndTombstone(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "history@example.test", auth.OrganizationInput{Mode: "create", Name: "History"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	path := "/api/traps/" + trap.ID
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	runtime.RuntimeState = "running"
	executeIntegrationCommand(t, r, a, ctx, identity, "start", runtime)
	trapMutation(t, r, a, "DELETE", path, "", 1, 409)
	runtime.RuntimeState = "stopped"
	stop := executeIntegrationCommand(t, r, a, ctx, identity, "stop", runtime)
	event := events.AgentEvent{EventID: string(contract.NewID()), EventType: "tcp.connection_opened", TypeID: trap.TypeID, TypeVersion: 1, ProfileRevision: 1, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), SessionID: string(contract.NewID()), SessionSequence: 1, Source: events.Source{IP: "192.0.2.1", Port: 50000}, Destination: events.Destination{Protocol: "tcp", Port: 2222}, Data: json.RawMessage(`{"listener_name":"ssh"}`)}
	raw := json.RawMessage(integrationJSON(t, event))
	batch := agentws.TelemetryBatch{BatchID: string(contract.NewID()), Events: []agentws.Event{{EventID: event.EventID, Raw: raw}}}
	before := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	ack, err := r.Agents.Ingest(ctx, identity, connection, batch)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := r.Agents.Ingest(ctx, identity, connection, batch)
	if err != nil || !repeated.StoredAt.Equal(ack.StoredAt) {
		t.Fatalf("replay=%+v %v", repeated, err)
	}
	current := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	if !current.LastSeenAt.Equal(*before.LastSeenAt) || current.StateVersion != before.StateVersion {
		t.Fatal("telemetry refreshed heartbeat")
	}
	// Corrected content under an accepted event ID conflicts atomically.
	changed := event
	changed.Source.Port = 50001
	changedRaw := json.RawMessage(integrationJSON(t, changed))
	_, err = r.Agents.Ingest(ctx, identity, connection, agentws.TelemetryBatch{BatchID: string(contract.NewID()), Events: []agentws.Event{{EventID: event.EventID, Raw: changedRaw}}})
	if err == nil {
		t.Fatal("event conflict accepted")
	}
	trapMutation(t, r, a, "DELETE", path, "", 1, 204)
	details := sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", a, 200)
	if decodeIntegration[events.Event](t, details).TrapID != trap.ID {
		t.Fatal("history owner lost")
	}
	sendOperator(t, r, "GET", "/api/events?trap_id="+trap.ID, "", a, 200)
	sendOperator(t, r, "GET", path+"/commands", "", a, 200)
	sendOperator(t, r, "GET", path+"/commands/"+stop.ID, "", a, 200)
	sendOperator(t, r, "POST", path+"/commands", integrationJSON(t, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "start", Params: json.RawMessage(`{}`)}), a, 404)
	foreign := registerOperator(t, r, "foreign@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", foreign, 404)
	sendOperator(t, r, "GET", "/api/events?trap_id="+trap.ID, "", foreign, 404)
	sendOperator(t, r, "GET", path+"/commands", "", foreign, 404)
}
func TestConcurrentStartAndDelete(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "race@example.test", auth.OrganizationInput{Mode: "create", Name: "Race"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, _, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	path := "/api/traps/" + trap.ID
	start := operatorRequest(t, "POST", path+"/commands", integrationJSON(t, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "start", Params: json.RawMessage(`{}`)}), a)
	deletion := operatorRequest(t, "DELETE", path, "", a)
	deletion.Header.Set("X-Expected-Revision", "1")
	ready := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	sw, dw := httptest.NewRecorder(), httptest.NewRecorder()
	go func() { defer wg.Done(); <-ready; r.Router.ServeHTTP(sw, start) }()
	go func() { defer wg.Done(); <-ready; r.Router.ServeHTTP(dw, deletion) }()
	close(ready)
	wg.Wait()
	if !(sw.Code == 201 && dw.Code == 409 || sw.Code == 404 && dw.Code == 204) {
		t.Fatalf("start=%d %s/delete=%d %s", sw.Code, sw.Body.String(), dw.Code, dw.Body.String())
	}
}
