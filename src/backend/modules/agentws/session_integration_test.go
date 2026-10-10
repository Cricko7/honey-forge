//go:build integration

package agentws

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

// A TLS WebSocket exercises the real handshake, session fencing and token
// revocation with temporary implementations of the absent 05/07 boundaries.
func TestReconnectAndRevocationIntegration(t *testing.T) {
	gateway := &fakeGateway{token: "secret"}
	url, closeServer := startAgentServer(t, gateway, &fakeCommands{})
	defer closeServer()

	first, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	sendAgent(t, first, "agent.hello", helloPayload())
	firstWelcome := readAgent(t, first, "agent.welcome")

	second, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	sendAgent(t, second, "agent.hello", helloPayload())
	secondWelcome := readAgent(t, second, "agent.welcome")
	if string(firstWelcome["payload"]) == string(secondWelcome["payload"]) {
		t.Fatal("reconnect reused the connection identifier")
	}

	var closed *websocket.CloseError
	if err := first.ReadJSON(&map[string]any{}); !errors.As(err, &closed) || closed.Code != 4409 {
		t.Fatalf("old connection was not replaced: %v", err)
	}

	sendAgent(t, second, "agent.heartbeat", map[string]any{"runtime": helloPayload()["runtime"]})
	readAgent(t, second, "agent.heartbeat_ack")

	gateway.mu.Lock()
	gateway.token = "revoked"
	gateway.mu.Unlock()
	sendAgent(t, second, "agent.heartbeat", map[string]any{"runtime": helloPayload()["runtime"]})
	closed = nil
	if err := second.ReadJSON(&map[string]any{}); !errors.As(err, &closed) || closed.Code != 4401 {
		t.Fatalf("revoked token was not closed: %v", err)
	}
}

func TestInvalidHelloIntegration(t *testing.T) {
	gateway := &fakeGateway{token: "secret"}
	url, closeServer := startAgentServer(t, gateway, &fakeCommands{})
	defer closeServer()

	conn, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	hello := helloPayload()
	hello["supported_types"] = []any{map[string]any{"type_id": "other", "type_version": 1, "actions": []string{"start"}}}
	sendAgent(t, conn, "agent.hello", hello)
	message := readAgent(t, conn, "error")
	if string(message["payload"]) == "" {
		t.Fatal("missing unsupported type error")
	}

	var closed *websocket.CloseError
	if err := conn.ReadJSON(&map[string]any{}); !errors.As(err, &closed) || closed.Code != 4400 {
		t.Fatalf("invalid hello close: %v", err)
	}

}

func TestLostAckRecoveryIntegration(t *testing.T) {
	revision := int32(2)
	gateway := &fakeGateway{token: "secret", state: State{
		CurrentConfiguration: &profiles.Snapshot{ProfileID: string(contract.NewID()), ProfileRevision: 1, TypeID: "tcp-banner", TypeVersion: 1, Config: profiles.Object{}},
		DesiredState:         "stopped", HeartbeatIntervalSeconds: 10,
	}}
	dispatch := &commands.Dispatch{CommandID: string(contract.NewID()), LeaseID: string(contract.NewID()), Action: "apply_config", Params: json.RawMessage(`{"profile_revision":2}`), LeaseExpiresAt: time.Now().Add(time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	commandService := &fakeCommands{dispatch: dispatch, recorded: make(chan struct{}, 2)}
	url, closeServer := startAgentServer(t, gateway, commandService)
	defer closeServer()

	first, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}

	hello := helloPayload()
	hello["runtime"].(map[string]any)["applied_profile_revision"] = revision
	sendAgent(t, first, "agent.hello", hello)
	readAgent(t, first, "agent.welcome")
	readAgent(t, first, "command.dispatch")

	result := map[string]any{"command_id": dispatch.CommandID, "lease_id": dispatch.LeaseID,
		"status": "succeeded", "result": map[string]any{"runtime_state": "stopped", "applied_profile_revision": revision},
		"error": nil, "runtime": hello["runtime"]}
	sendAgent(t, first, "command.result", result)
	select {
	case <-commandService.recorded:
	case <-time.After(2 * time.Second):
		t.Fatal("result not recorded")
	}
	first.Close() // Simulate losing command.ack after the result was recorded.

	second, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	sendAgent(t, second, "agent.hello", hello)
	welcome := readAgent(t, second, "agent.welcome")
	if !json.Valid(welcome["payload"]) || !containsRevision(welcome["payload"], 1) {
		t.Fatalf("expected older center snapshot in welcome: %s", welcome["payload"])
	}

	sendAgent(t, second, "command.result", result)
	readAgent(t, second, "command.ack")
	select {
	case <-commandService.recorded:
	case <-time.After(2 * time.Second):
		t.Fatal("replayed result not accepted")
	}

	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if len(gateway.observed) < 3 || gateway.observed[len(gateway.observed)-1].AppliedProfileRevision == nil || *gateway.observed[len(gateway.observed)-1].AppliedProfileRevision != revision {
		t.Fatal("replayed runtime was rolled back")
	}
}

func containsRevision(raw json.RawMessage, revision int32) bool {
	var welcome struct {
		CurrentConfiguration struct {
			ProfileRevision int32 `json:"profile_revision"`
		} `json:"current_configuration"`
	}

	return json.Unmarshal(raw, &welcome) == nil && welcome.CurrentConfiguration.ProfileRevision == revision
}
