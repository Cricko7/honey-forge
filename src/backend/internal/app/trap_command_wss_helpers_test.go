//go:build integration

package app

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/commands"
	"honey-forge/modules/traps"
)

func connectCommandWSS(t *testing.T, endpoint, token string, trap traps.Trap, runtime commands.AgentRuntime) (*websocket.Conn, map[string]json.RawMessage) {
	t.Helper()
	// TLS verification is disabled only for httptest's self-signed certificate.
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Subprotocols: []string{"resource-stream.v1"}}
	socket, response, err := dialer.Dial(endpoint, http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		t.Fatalf("agent handshake: %v (%v)", err, response)
	}
	t.Cleanup(func() {
		if err := socket.Close(); err != nil {
			t.Logf("close agent: %v", err)
		}
	})
	hello := agentws.AgentHello{BootID: string(contract.NewID()), AgentVersion: "integration", Hostname: "test-decoy", SupportedTypes: []agentws.SupportedType{{TypeID: trap.TypeID, TypeVersion: trap.TypeVersion, Actions: []string{"apply_config", "start", "stop"}}}, Runtime: runtime}
	id := sendCommandWSS(t, socket, "agent.hello", hello)
	return socket, readCommandWSS(t, socket, "agent.welcome", id)
}

func sendCommandWSS(t *testing.T, socket *websocket.Conn, typ string, payload any) string {
	t.Helper()
	id := string(contract.NewID())
	if err := socket.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := socket.WriteJSON(map[string]any{"message_id": id, "type": typ, "payload": payload}); err != nil {
		t.Fatal(err)
	}
	return id
}

func readCommandWSS(t *testing.T, socket *websocket.Conn, typ, replyTo string) map[string]json.RawMessage {
	t.Helper()
	if err := socket.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message contract.Envelope
	if err := socket.ReadJSON(&message); err != nil {
		t.Fatalf("read %s: %v", typ, err)
	}
	if message.Type != typ || replyTo != "" && (message.ReplyTo == nil || string(*message.ReplyTo) != replyTo) {
		t.Fatalf("expected %s reply_to=%s, got %+v", typ, replyTo, message)
	}
	return message.Payload
}

func payloadCommandValue[T any](t *testing.T, payload map[string]json.RawMessage, field string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(payload[field], &value); err != nil {
		t.Fatalf("decode %s: %v", field, err)
	}
	return value
}

func readCommandDispatch(t *testing.T, socket *websocket.Conn, command commands.Command) commands.Dispatch {
	t.Helper()
	payload := readCommandWSS(t, socket, "command.dispatch", "")
	var dispatch commands.Dispatch
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &dispatch); err != nil {
		t.Fatal(err)
	}
	if dispatch.CommandID != command.ID || dispatch.Action != command.Action || dispatch.LeaseID == "" {
		t.Fatalf("dispatch differs from REST command: %+v", dispatch)
	}
	return dispatch
}

func commandResultPayload(dispatch commands.Dispatch, runtime commands.AgentRuntime, failure *commands.RuntimeError) map[string]any {
	var result any
	status := commands.Failed
	if failure == nil {
		status = commands.Succeeded
		result = map[string]any{"runtime_state": runtime.RuntimeState, "applied_profile_revision": runtime.AppliedProfileRevision}
	}
	return map[string]any{"command_id": dispatch.CommandID, "lease_id": dispatch.LeaseID, "status": status, "result": result, "error": failure, "runtime": runtime}
}
