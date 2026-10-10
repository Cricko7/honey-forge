//go:build integration

package app

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

// No command/gateway fakes: REST, TLS/WSS, migrations and PostgreSQL all use
// the production composition root. Only the remote agent is simulated.
func TestTrapCommandsWSSLifecycle(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "command-wss@example.test", auth.OrganizationInput{Mode: "create", Name: "Commands WSS"})
	p, trap, _ := createIntegrationTrap(t, r, a)
	path := "/api/traps/" + trap.ID
	commandPath := path + "/commands"
	readTrap := func() traps.Trap {
		return decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", a, 200))
	}
	createCommand := func(action string, params json.RawMessage) commands.Command {
		return decodeIntegration[commands.Command](t, sendOperator(t, r, "POST", commandPath, integrationJSON(t, commands.CreateRequest{RequestID: string(contract.NewID()), Action: action, Params: params}), a, 201))
	}
	readCommand := func(id string) commands.Command {
		return decodeIntegration[commands.Command](t, sendOperator(t, r, "GET", commandPath+"/"+id, "", a, 200))
	}
	startBody := `{"request_id":"` + string(contract.NewID()) + `","action":"start","params":{}}`
	requireOperatorError(t, sendOperator(t, r, "POST", commandPath, startBody, a, 409), "configuration_not_applied")
	apply := createCommand("apply_config", json.RawMessage(`{"profile_revision":1}`))
	queued := readTrap()
	if queued.Connectivity != "offline" || queued.ActiveCommandID == nil || *queued.ActiveCommandID != apply.ID || queued.DesiredProfileRevision == nil || *queued.DesiredProfileRevision != 1 {
		t.Fatal("offline apply was not queued atomically")
	}
	requireOperatorError(t, sendOperator(t, r, "POST", commandPath, startBody, a, 409), "command_in_progress")
	// Editing a profile after queueing must not change the pinned snapshot.
	config := validIntegrationConfig()
	config["listeners"].([]any)[0].(profiles.Object)["banner"] = "new revision"
	updated := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "PATCH", "/api/profiles/"+p.ID, integrationJSON(t, profiles.Object{"config": config}), a, 200, profiles.ETag(p)))
	if updated.Revision != 2 {
		t.Fatal("profile revision did not advance")
	}
	credentials := decodeIntegration[traps.AgentCredentials](t, sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":0}`, a, 200))
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream"
	runtime := commands.AgentRuntime{RuntimeState: "stopped", BufferCapacityBytes: 1024, BufferState: "ok"}
	socket, welcome := connectCommandWSS(t, endpoint, credentials.Token, trap, runtime)
	if string(welcome["current_configuration"]) != "null" {
		t.Fatal("registration applied configuration implicitly")
	}
	dispatch := readCommandDispatch(t, socket, apply)
	if dispatch.Configuration == nil || dispatch.Configuration.ProfileRevision != 1 || !profiles.EqualJSON(dispatch.Configuration.Config, p.Config) || readCommand(apply.ID).Status != commands.Running {
		t.Fatal("dispatch did not preserve the queued snapshot")
	}
	progressID := sendCommandWSS(t, socket, "command.progress", map[string]any{"command_id": apply.ID, "lease_id": dispatch.LeaseID})
	readCommandWSS(t, socket, "command.progress_ack", progressID)
	before := readTrap()
	var revision int32 = 1
	runtime.AppliedProfileRevision = &revision
	result := commandResultPayload(dispatch, runtime, nil)
	result["result"] = map[string]any{"runtime_state": "stopped", "applied_profile_revision": 2}
	invalidID := sendCommandWSS(t, socket, "command.result", result)
	invalid := readCommandWSS(t, socket, "error", invalidID)
	if payloadCommandValue[commands.RuntimeError](t, invalid, "error").Code != "command_result_invalid" || readCommand(apply.ID).Status != commands.Running || readTrap().StateVersion != before.StateVersion {
		t.Fatal("invalid result was accepted or partially committed")
	}
	result = commandResultPayload(dispatch, runtime, nil)
	resultID := sendCommandWSS(t, socket, "command.result", result)
	ack := readCommandWSS(t, socket, "command.ack", resultID)
	applied := readTrap()
	if applied.ActiveCommandID != nil || applied.AppliedProfileRevision == nil || *applied.AppliedProfileRevision != 1 || applied.RuntimeState != "stopped" || applied.Revision != 1 || !applied.UpdatedAt.Equal(trap.UpdatedAt) || !applied.LastSeenAt.Equal(*before.LastSeenAt) || applied.StateVersion != before.StateVersion+1 {
		t.Fatal("command completion changed the wrong trap fields")
	}
	replayID := sendCommandWSS(t, socket, "command.result", result)
	replayedAck := readCommandWSS(t, socket, "command.ack", replayID)
	if string(ack["recorded_at"]) != string(replayedAck["recorded_at"]) {
		t.Fatalf("replayed recorded_at changed: first=%s replay=%s", ack["recorded_at"], replayedAck["recorded_at"])
	}
	if readTrap().StateVersion != applied.StateVersion {
		t.Fatal("replayed result changed trap state_version")
	}
	// Reconnect must receive the installed revision, not the edited Profile.
	second, recovered := connectCommandWSS(t, endpoint, credentials.Token, trap, runtime)
	currentConfig := payloadCommandValue[profiles.Snapshot](t, recovered, "current_configuration")
	if currentConfig.ProfileRevision != 1 || !profiles.EqualJSON(currentConfig.Config, p.Config) {
		t.Fatal("reconnect adopted an unapplied profile revision")
	}
	var closed *websocket.CloseError
	if _, _, err := socket.ReadMessage(); !errors.As(err, &closed) || closed.Code != 4409 {
		t.Fatalf("old session survived reconnect: %v", err)
	}
	socket = second
	start := createCommand("start", json.RawMessage(`{}`))
	dispatch = readCommandDispatch(t, socket, start)
	if dispatch.Configuration != nil || readTrap().DesiredState != "running" {
		t.Fatal("start reapplied configuration or lost desired state")
	}
	runtime.RuntimeState = "running"
	resultID = sendCommandWSS(t, socket, "command.result", commandResultPayload(dispatch, runtime, nil))
	readCommandWSS(t, socket, "command.ack", resultID)
	if current := readTrap(); current.RuntimeState != "running" || *current.AppliedProfileRevision != 1 {
		t.Fatal("start did not use the installed revision")
	}
	stop := createCommand("stop", json.RawMessage(`{}`))
	dispatch = readCommandDispatch(t, socket, stop)
	requireOperatorError(t, trapMutation(t, r, a, "DELETE", path, "", 1, 409), "command_in_progress")
	failed := commandResultPayload(dispatch, runtime, &commands.RuntimeError{Code: "runtime_stop_failed", Message: "unsafe agent detail"})
	resultID = sendCommandWSS(t, socket, "command.result", failed)
	readCommandWSS(t, socket, "command.ack", resultID)
	if outcome := readCommand(stop.ID); outcome.Status != commands.Failed || outcome.Error == nil || outcome.Error.Message == "unsafe agent detail" {
		t.Fatal("failed stop outcome was lost or exposed agent details")
	}
	requireOperatorError(t, trapMutation(t, r, a, "DELETE", path, "", 1, 409), "trap_not_stopped")
	stop = createCommand("stop", json.RawMessage(`{}`))
	dispatch = readCommandDispatch(t, socket, stop)
	runtime.RuntimeState, runtime.BufferedEvents, runtime.BufferBytes = "stopped", 1, 100
	resultID = sendCommandWSS(t, socket, "command.result", commandResultPayload(dispatch, runtime, nil))
	readCommandWSS(t, socket, "command.ack", resultID)
	requireOperatorError(t, trapMutation(t, r, a, "DELETE", path, "", 1, 409), "trap_buffer_not_empty")
	event := events.AgentEvent{EventID: string(contract.NewID()), EventType: "tcp.connection_opened", TypeID: trap.TypeID, TypeVersion: 1, ProfileRevision: 1, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), SessionID: string(contract.NewID()), SessionSequence: 1, Source: events.Source{IP: "192.0.2.1", Port: 50000}, Destination: events.Destination{Protocol: "tcp", Port: 2222}, Data: json.RawMessage(`{"listener_name":"ssh"}`)}
	before = readTrap()
	batchID := string(contract.NewID())
	messageID := sendCommandWSS(t, socket, "telemetry.batch", map[string]any{"batch_id": batchID, "events": []events.AgentEvent{event}})
	telemetryAck := readCommandWSS(t, socket, "telemetry.ack", messageID)
	if payloadCommandValue[string](t, telemetryAck, "batch_id") != batchID || !readTrap().LastSeenAt.Equal(*before.LastSeenAt) {
		t.Fatal("telemetry changed heartbeat or did not acknowledge the batch")
	}
	runtime.BufferedEvents, runtime.BufferBytes = 0, 0
	messageID = sendCommandWSS(t, socket, "agent.heartbeat", map[string]any{"runtime": runtime})
	readCommandWSS(t, socket, "agent.heartbeat_ack", messageID)
	trapMutation(t, r, a, "DELETE", path, "", 1, 204)
	if _, _, err := socket.ReadMessage(); !errors.As(err, &closed) || closed.Code != 4401 {
		t.Fatalf("deleted trap WSS remained authenticated: %v", err)
	}
	sendOperator(t, r, "GET", path, "", a, 404)
	sendOperator(t, r, "GET", commandPath+"/"+stop.ID, "", a, 200)
	sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", a, 200)
	foreign := registerOperator(t, r, "command-foreign@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	sendOperator(t, r, "GET", commandPath+"/"+stop.ID, "", foreign, 404)
	join := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", a, 200))
	viewer := registerOperator(t, r, "command-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: join.Code})
	sendOperator(t, r, "GET", commandPath, "", viewer, 200)
	sendOperator(t, r, "POST", commandPath, startBody, viewer, 403)
}
