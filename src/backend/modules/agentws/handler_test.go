package agentws

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
)

const testTrapID = "11111111-1111-4111-8111-111111111111"
const testOrgID = "22222222-2222-4222-8222-222222222222"

type fakeGateway struct {
	mu            sync.Mutex
	token         string
	observed      []commands.AgentRuntime
	batches       []TelemetryBatch
	offline       int
	ingestStarted chan struct{}
	ingestRelease chan struct{}
	state         State
}

func (f *fakeGateway) Authenticate(_ context.Context, token string) (Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if token != f.token {
		return Identity{}, contract.NewError("agent_unauthenticated")
	}

	return Identity{OrganizationID: testOrgID, TrapID: testTrapID, TypeID: "tcp-banner", TypeVersion: 1, RequiredActions: []string{"start", "stop", "apply_config"}}, nil
}

func (f *fakeGateway) Observe(_ context.Context, _ Identity, _ string, runtime commands.AgentRuntime) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.observed = append(f.observed, runtime)
	if f.state.DesiredState != "" {
		return f.state, nil
	}
	return State{DesiredState: "stopped", HeartbeatIntervalSeconds: 10}, nil
}

func (f *fakeGateway) Offline(_ context.Context, _ Identity, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.offline++
	return nil
}

func (f *fakeGateway) Ingest(_ context.Context, _ Identity, _ string, batch TelemetryBatch) (TelemetryAck, error) {
	f.mu.Lock()
	f.batches = append(f.batches, batch)
	f.mu.Unlock()

	if f.ingestStarted != nil {
		f.ingestStarted <- struct{}{}
		<-f.ingestRelease
	}

	ids := make([]string, len(batch.Events))
	for i, event := range batch.Events {
		ids[i] = event.EventID
	}

	return TelemetryAck{BatchID: batch.BatchID, AcknowledgedEventIDs: ids, StoredAt: time.Now().UTC()}, nil
}

type fakeCommands struct {
	mu         sync.Mutex
	dispatch   *commands.Dispatch
	dispatches []*commands.Dispatch
	result     commands.AgentResult
	recorded   chan struct{}
}

func (f *fakeCommands) Claim(context.Context, string, string) (*commands.Dispatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	dispatch := f.dispatch
	f.dispatch = nil
	if dispatch == nil && len(f.dispatches) > 0 {
		dispatch = f.dispatches[0]
		f.dispatches = f.dispatches[1:]
	}
	return dispatch, nil
}

func (f *fakeCommands) ExtendLease(context.Context, string, string, string, string) (time.Time, error) {
	return time.Now().Add(time.Minute).UTC(), nil
}

func (f *fakeCommands) RecordResult(_ context.Context, _, _ string, result commands.AgentResult) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.result = result
	if f.recorded != nil {
		f.recorded <- struct{}{}
	}
	return time.Now().UTC(), nil
}

func startAgentServer(t *testing.T, gateway *fakeGateway, commands *fakeCommands) (string, func()) {
	t.Helper()

	router := gin.New()
	router.Use(contract.Middleware())
	NewHandler(gateway, commands).Register(router)
	server := httptest.NewTLSServer(router)
	return "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream", server.Close
}

func dialAgent(t *testing.T, url, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()

	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Subprotocols: []string{"resource-stream.v1"}}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	return dialer.Dial(url, header)
}

func sendAgent(t *testing.T, conn *websocket.Conn, kind string, payload any) {
	t.Helper()

	message := map[string]any{"message_id": contract.NewID(), "type": kind, "payload": payload}
	if err := conn.WriteJSON(message); err != nil {
		t.Fatal(err)
	}
}

func readAgent(t *testing.T, conn *websocket.Conn, want string) map[string]json.RawMessage {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message map[string]json.RawMessage
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if string(message["type"]) != `"`+want+`"` {
		t.Fatalf("got message %s, want %s", message["type"], want)
	}

	return message
}

func helloPayload() map[string]any {
	return map[string]any{
		"boot_id": contract.NewID(), "agent_version": "0.1.0", "hostname": "decoy-01",
		"supported_types": []any{map[string]any{"type_id": "tcp-banner", "type_version": 1, "actions": []string{"start", "stop", "apply_config"}}},
		"runtime":         map[string]any{"runtime_state": "stopped", "applied_profile_revision": nil, "buffered_events": 0, "buffer_bytes": 0, "buffer_capacity_bytes": 1024, "buffer_state": "ok", "last_error": nil},
	}
}

func TestHandshakeAndHello(t *testing.T) {
	gateway := &fakeGateway{token: "secret"}
	url, closeServer := startAgentServer(t, gateway, &fakeCommands{})
	defer closeServer()

	for _, tt := range []struct {
		name   string
		token  string
		status int
	}{{"missing token", "", 401}, {"invalid token", "wrong", 401}, {"valid token", "secret", 101}} {
		t.Run(tt.name, func(t *testing.T) {
			conn, response, err := dialAgent(t, url, tt.token)
			if response == nil || response.StatusCode != tt.status {
				t.Fatalf("status %v, error %v", response, err)
			}
			if conn == nil {
				return
			}
			defer conn.Close()

			sendAgent(t, conn, "agent.hello", helloPayload())
			welcome := readAgent(t, conn, "agent.welcome")
			if len(welcome["payload"]) == 0 || conn.Subprotocol() != "resource-stream.v1" {
				t.Fatal("missing welcome or negotiated subprotocol")
			}
		})
	}
}

func TestHandshakeRejectsBrowserAndWrongProtocol(t *testing.T) {
	url, closeServer := startAgentServer(t, &fakeGateway{token: "secret"}, &fakeCommands{})
	defer closeServer()

	for _, tt := range []struct {
		name     string
		protocol string
		origin   string
		status   int
	}{
		{"missing protocol", "", "", 400},
		{"wrong protocol", "other", "", 400},
		{"browser origin", "resource-stream.v1", "https://operator.example", 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
			if tt.protocol != "" {
				dialer.Subprotocols = []string{tt.protocol}
			}

			header := http.Header{}
			header.Set("Authorization", "Bearer secret")
			if tt.origin != "" {
				header.Set("Origin", tt.origin)
			}

			conn, response, err := dialer.Dial(url, header)
			if conn != nil {
				conn.Close()
				t.Fatal("invalid handshake upgraded")
			}
			if response == nil || response.StatusCode != tt.status || err == nil {
				t.Fatalf("status=%v error=%v", response, err)
			}
		})
	}
}

func TestMissingDependenciesReturnUnavailable(t *testing.T) {
	router := gin.New()
	router.Use(contract.Middleware())
	NewHandler(nil, nil).Register(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/assets/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestCommandAndTelemetry(t *testing.T) {
	gateway := &fakeGateway{token: "secret"}
	dispatch := &commands.Dispatch{CommandID: string(contract.NewID()), LeaseID: string(contract.NewID()), Action: "stop", Params: json.RawMessage(`{}`), LeaseExpiresAt: time.Now().Add(time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	cmd := &fakeCommands{dispatch: dispatch}
	url, closeServer := startAgentServer(t, gateway, cmd)
	defer closeServer()

	conn, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendAgent(t, conn, "agent.hello", helloPayload())
	readAgent(t, conn, "agent.welcome")
	readAgent(t, conn, "command.dispatch")

	runtime := helloPayload()["runtime"]
	sendAgent(t, conn, "command.progress", map[string]any{"command_id": dispatch.CommandID, "lease_id": dispatch.LeaseID})
	readAgent(t, conn, "command.progress_ack")

	sendAgent(t, conn, "command.result", map[string]any{"command_id": dispatch.CommandID, "lease_id": dispatch.LeaseID, "status": "succeeded", "result": map[string]any{"runtime_state": "stopped", "applied_profile_revision": nil}, "error": nil, "runtime": runtime})
	readAgent(t, conn, "command.ack")

	sendAgent(t, conn, "agent.heartbeat", map[string]any{"runtime": runtime})
	readAgent(t, conn, "agent.heartbeat_ack")

	eventID := string(contract.NewID())
	sendAgent(t, conn, "telemetry.batch", map[string]any{"batch_id": contract.NewID(), "events": []any{map[string]any{"event_id": eventID}}})
	readAgent(t, conn, "telemetry.ack")
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	cmd.mu.Lock()
	defer cmd.mu.Unlock()
	if len(gateway.batches) != 1 || len(gateway.observed) != 3 || cmd.result.CommandID != dispatch.CommandID {
		t.Fatalf("batches=%d observed=%d", len(gateway.batches), len(gateway.observed))
	}
}

func TestSlowTelemetryDoesNotBlockHeartbeat(t *testing.T) {
	gateway := &fakeGateway{token: "secret", ingestStarted: make(chan struct{}, 1), ingestRelease: make(chan struct{})}
	url, closeServer := startAgentServer(t, gateway, &fakeCommands{})
	defer closeServer()

	conn, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendAgent(t, conn, "agent.hello", helloPayload())
	readAgent(t, conn, "agent.welcome")

	batch := map[string]any{"batch_id": contract.NewID(), "events": []any{map[string]any{"event_id": contract.NewID()}}}
	sendAgent(t, conn, "telemetry.batch", batch)
	select {
	case <-gateway.ingestStarted:
	case <-time.After(time.Second):
		t.Fatal("ingest did not start")
	}

	sendAgent(t, conn, "agent.heartbeat", map[string]any{"runtime": helloPayload()["runtime"]})
	readAgent(t, conn, "agent.heartbeat_ack")

	sendAgent(t, conn, "telemetry.batch", batch)
	readAgent(t, conn, "error")

	close(gateway.ingestRelease)
	readAgent(t, conn, "telemetry.ack")
}

func TestInvalidMessageKeepsConnection(t *testing.T) {
	url, closeServer := startAgentServer(t, &fakeGateway{token: "secret"}, &fakeCommands{})
	defer closeServer()

	conn, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendAgent(t, conn, "agent.hello", helloPayload())
	readAgent(t, conn, "agent.welcome")

	id := string(contract.NewID())
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"message_id":"`+id+`","type":"agent.heartbeat","payload":{},"unexpected":true}`)); err != nil {
		t.Fatal(err)
	}
	message := readAgent(t, conn, "error")
	if string(message["reply_to"]) != `"`+id+`"` {
		t.Fatalf("wrong reply_to: %s", message["reply_to"])
	}

	sendAgent(t, conn, "agent.heartbeat", map[string]any{"runtime": helloPayload()["runtime"]})
	readAgent(t, conn, "agent.heartbeat_ack")
}

func TestExpiredLeaseIsRedispatched(t *testing.T) {
	commandID := string(contract.NewID())
	first := &commands.Dispatch{CommandID: commandID, LeaseID: string(contract.NewID()), Action: "stop", Params: json.RawMessage(`{}`), LeaseExpiresAt: time.Now().Add(100 * time.Millisecond), ExpiresAt: time.Now().Add(time.Hour)}
	second := &commands.Dispatch{CommandID: commandID, LeaseID: string(contract.NewID()), Action: "stop", Params: json.RawMessage(`{}`), LeaseExpiresAt: time.Now().Add(time.Minute), ExpiresAt: first.ExpiresAt}
	url, closeServer := startAgentServer(t, &fakeGateway{token: "secret"}, &fakeCommands{dispatches: []*commands.Dispatch{first, second}})
	defer closeServer()

	conn, _, err := dialAgent(t, url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendAgent(t, conn, "agent.hello", helloPayload())
	readAgent(t, conn, "agent.welcome")
	firstMessage := readAgent(t, conn, "command.dispatch")
	secondMessage := readAgent(t, conn, "command.dispatch")
	if string(firstMessage["payload"]) == string(secondMessage["payload"]) {
		t.Fatal("lease was not renewed")
	}
}
