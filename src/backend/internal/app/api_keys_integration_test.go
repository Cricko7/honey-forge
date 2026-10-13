//go:build integration

package app

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
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

type issuedAPIKey struct {
	auth.APIKey
	Token string `json:"token"`
}

func keyRequest(t *testing.T, r *Runtime, token, method, path, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	w := serveOperator(r.Router, request)
	if w.Code != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, w.Code, w.Body.String(), want)
	}

	return w
}

func TestAPIKeyRESTRealtimeAndTrapStart(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "key-demo@example.test", auth.OrganizationInput{Mode: "create", Name: "Keys demo"})
	key := decodeIntegration[issuedAPIKey](t, sendOperator(t, r, "POST", "/api/api-keys", `{"name":"Integration"}`, admin, 201))
	foreign := registerOperator(t, r, "key-foreign@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	_, foreignTrap, _ := createIntegrationTrap(t, r, foreign)
	keyRequest(t, r, key.Token, "GET", "/api/traps/"+foreignTrap.ID, "", 404)
	sendOperator(t, r, "DELETE", "/api/api-keys/"+key.ID, "", foreign, 404)
	keyRequest(t, r, key.Token, "POST", "/api/api-keys", `{"name":"Escalation"}`, 403)

	code := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, r, "key-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: code.Code})
	sendOperator(t, r, "POST", "/api/api-keys", `{"name":"Forbidden"}`, viewer, 403)
	sendOperator(t, r, "GET", "/api/api-keys", "", viewer, 403)
	sendOperator(t, r, "DELETE", "/api/api-keys/"+key.ID, "", viewer, 403)

	page := decodeIntegration[events.EventPage](t, keyRequest(t, r, key.Token, "GET", "/api/events", "", 200))
	p := decodeIntegration[profiles.Profile](t, keyRequest(t, r, key.Token, "POST", "/api/profiles", integrationJSON(t, catalogProfileRequest(t, r, admin)), 201))
	trap := decodeIntegration[traps.Trap](t, keyRequest(t, r, key.Token, "POST", "/api/traps",
		integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "API trap", ProfileID: p.ID}), 201))
	path := "/api/traps/" + trap.ID
	apply := decodeIntegration[commands.Command](t, keyRequest(t, r, key.Token, "POST", path+"/commands",
		integrationJSON(t, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "apply_config", Params: json.RawMessage(`{"profile_revision":1}`)}), 201))
	credentials := decodeIntegration[traps.AgentCredentials](t, keyRequest(t, r, key.Token, "POST", path+"/agent-credentials", `{"expected_generation":0}`, 200))
	server := httptest.NewTLSServer(r.Router)
	defer server.Close()

	endpoint := "wss" + strings.TrimPrefix(server.URL, "https")
	agentRuntime := commands.AgentRuntime{RuntimeState: "stopped", BufferCapacityBytes: 1024, BufferState: "ok"}
	agent, _ := connectCommandWSS(t, endpoint+"/assets/stream", credentials.Token, trap, agentRuntime)
	dispatch := readCommandDispatch(t, agent, apply)
	var revision int32 = 1
	agentRuntime.AppliedProfileRevision = &revision
	resultID := sendCommandWSS(t, agent, "command.result", commandResultPayload(dispatch, agentRuntime, nil))
	readCommandWSS(t, agent, "command.ack", resultID)
	start := decodeIntegration[commands.Command](t, keyRequest(t, r, key.Token, "POST", path+"/commands",
		integrationJSON(t, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "start", Params: json.RawMessage(`{}`)}), 201))
	dispatch = readCommandDispatch(t, agent, start)
	agentRuntime.RuntimeState = "running"
	resultID = sendCommandWSS(t, agent, "command.result", commandResultPayload(dispatch, agentRuntime, nil))
	readCommandWSS(t, agent, "command.ack", resultID)
	running := decodeIntegration[traps.Trap](t, keyRequest(t, r, key.Token, "GET", path, "", 200))
	if running.RuntimeState != "running" {
		t.Fatal("API key command did not start the trap")
	}

	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Subprotocols: []string{"dashboard-stream.v1"}}
	socket, response, err := dialer.Dial(endpoint+"/api/stream", http.Header{"Authorization": []string{"Bearer " + key.Token}})
	if err != nil {
		if response != nil {
			t.Logf("handshake status: %d", response.StatusCode)
			if closeErr := response.Body.Close(); closeErr != nil {
				t.Error(closeErr)
			}
		}

		t.Fatal(err)
	}

	defer socket.Close()
	subscribeFrontend(t, socket, page.StreamCursor)
	seenTrap := false
	for {
		if err := socket.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}

		var message contract.Envelope
		if err := socket.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}

		if message.Type == "trap.changed" {
			var payload struct {
				Trap traps.Trap `json:"trap"`
			}
			if err := json.Unmarshal(message.Payload["data"], &payload); err != nil {
				t.Fatal(err)
			}

			if payload.Trap.ID == foreignTrap.ID {
				t.Fatal("foreign trap leaked into realtime")
			}

			seenTrap = true
		}

		if message.Type == "stream.ready" {
			break
		}
	}

	if !seenTrap {
		t.Fatal("realtime did not replay trap changes")
	}

	sendOperator(t, r, "DELETE", "/api/api-keys/"+key.ID, "", admin, 204)
	keyRequest(t, r, key.Token, "GET", "/api/traps", "", 401)
	if err := socket.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	_, _, err = socket.ReadMessage()
	var closed *websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != 4401 {
		t.Fatalf("revoked realtime connection: %v", err)
	}
}

func TestAPIKeyMinuteQuotaAndReset(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "key-quota@example.test", auth.OrganizationInput{Mode: "create", Name: "Quota"})
	key := decodeIntegration[issuedAPIKey](t, sendOperator(t, r, "POST", "/api/api-keys", `{"name":"Quota"}`, admin, 201))
	for range 60 {
		keyRequest(t, r, key.Token, "GET", "/api/organization", "", 200)
	}

	limited := keyRequest(t, r, key.Token, "GET", "/api/organization", "", 429)
	if limited.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}

	server := httptest.NewTLSServer(r.Router)
	defer server.Close()
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Subprotocols: []string{"dashboard-stream.v1"}}
	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/api/stream"
	socket, response, err := dialer.Dial(endpoint, http.Header{"Authorization": []string{"Bearer " + key.Token}})
	if socket != nil {
		if closeErr := socket.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}

	if response == nil {
		t.Fatalf("missing realtime rate response: %v", err)
	}

	if closeErr := response.Body.Close(); closeErr != nil {
		t.Error(closeErr)
	}

	if err == nil || response.StatusCode != 429 || response.Header.Get("Retry-After") == "" {
		t.Fatalf("realtime must share REST quota: %v, status %d", err, response.StatusCode)
	}

	other := decodeIntegration[issuedAPIKey](t, sendOperator(t, r, "POST", "/api/api-keys", `{"name":"Independent"}`, admin, 201))
	keyRequest(t, r, other.Token, "GET", "/api/organization", "", 200)
	if _, err := r.pool.Exec(t.Context(), `UPDATE operator_api_keys SET rate_window = now() - interval '1 minute' WHERE id = $1`, key.ID); err != nil {
		t.Fatal(err)
	}

	keyRequest(t, r, key.Token, "GET", "/api/organization", "", 200)
	socket, response, err = dialer.Dial(endpoint, http.Header{"Cookie": []string{"__Host-session=" + key.Token}, "Origin": []string{integrationOrigin}})
	if socket != nil {
		if closeErr := socket.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}

	if response == nil {
		t.Fatalf("missing cookie authentication response: %v", err)
	}

	if closeErr := response.Body.Close(); closeErr != nil {
		t.Error(closeErr)
	}

	if err == nil || response.StatusCode != 401 {
		t.Fatalf("key in cookie must not bypass quota: %v, status %d", err, response.StatusCode)
	}
}
