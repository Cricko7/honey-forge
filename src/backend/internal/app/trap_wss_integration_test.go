//go:build integration

package app

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	"honey-forge/modules/traps"
)

func TestTrapCredentialWSSRevocation(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "wss@example.test", auth.OrganizationInput{Mode: "create", Name: "WSS"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	path := "/api/traps/" + trap.ID
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream"
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Subprotocols: []string{"resource-stream.v1"}}
	connect := func(token string) *websocket.Conn {
		t.Helper()
		header := http.Header{"Authorization": []string{"Bearer " + token}}
		socket, response, err := dialer.Dial(endpoint, header)
		if err != nil {
			t.Fatalf("dial %v response %v", err, response)
		}
		t.Cleanup(func() {
			if err := socket.Close(); err != nil {
				t.Logf("close test socket: %v", err)
			}
		})
		hello := agentws.AgentHello{BootID: string(contract.NewID()), AgentVersion: "0.1", Hostname: "decoy", SupportedTypes: []agentws.SupportedType{{TypeID: trap.TypeID, TypeVersion: trap.TypeVersion, Actions: []string{"start", "stop", "apply_config"}}}, Runtime: commands.AgentRuntime{RuntimeState: "stopped", BufferState: "ok", BufferCapacityBytes: 1024}}
		if err := socket.WriteJSON(map[string]any{"message_id": contract.NewID(), "type": "agent.hello", "payload": hello}); err != nil {
			t.Fatal(err)
		}
		if err := socket.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var welcome struct {
			Type string `json:"type"`
		}
		if err := socket.ReadJSON(&welcome); err != nil || welcome.Type != "agent.welcome" {
			t.Fatalf("welcome %+v %v", welcome, err)
		}
		return socket
	}
	credentials := decodeIntegration[traps.AgentCredentials](t, sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":0}`, a, 200))
	socket := connect(credentials.Token)
	second := decodeIntegration[traps.AgentCredentials](t, sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":1}`, a, 200))
	_, _, err := socket.ReadMessage()
	var closeError *websocket.CloseError
	if !errors.As(err, &closeError) || closeError.Code != 4401 {
		t.Fatalf("rotation close: %v", err)
	}
	response, err := r.Agents.Authenticate(t.Context(), credentials.Token)
	if err == nil {
		t.Fatalf("old token authenticated: %+v", response)
	}
	socket2 := connect(second.Token)
	trapMutation(t, r, a, "DELETE", path, "", 1, 204)
	_, _, err = socket2.ReadMessage()
	if !errors.As(err, &closeError) || closeError.Code != 4401 {
		t.Fatalf("delete close: %v", err)
	}
	header := http.Header{"Authorization": []string{"Bearer " + second.Token}}
	failed, handshake, err := dialer.Dial(endpoint, header)
	if failed != nil {
		failed.Close()
		t.Fatal("tombstone connected")
	}
	if err == nil || handshake == nil || handshake.StatusCode != 401 {
		t.Fatalf("tombstone handshake: %v %v", handshake, err)
	}
	if handshake.Body != nil {
		if err := handshake.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
