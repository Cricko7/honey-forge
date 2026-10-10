//go:build integration

package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/agent"
	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

// Exercises the actual agent process, TCP listener, Kafka journal, and database.
func TestDemoRegistrationToTCPStop(t *testing.T) {
	brokers := os.Getenv("TEST_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("TEST_KAFKA_BROKERS required")
	}
	publisher, err := events.NewKafkaPublisher(strings.Split(brokers, ","), "honey-forge-e2e-"+string(contract.NewID()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)

	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	endpoint := "wss://" + server.Listener.Addr().String() + "/assets/stream"
	r := openIntegrationRuntimeWithOptions(t, nil, publisher, endpoint)
	server.Config.Handler = r.Router
	server.StartTLS()
	defer server.Close()

	registered := registerOperator(t, r, "demo-e2e@example.test", auth.OrganizationInput{Mode: "create", Name: "Demo E2E"})
	sendOperator(t, r, "DELETE", "/api/session", "", registered, 204)
	login := sendOperator(t, r, "POST", "/api/sessions", integrationJSON(t, auth.LoginRequest{Email: registered.view.User.Email, Password: integrationPassword}), operatorSession{}, 201)
	admin := sessionFromResponse(t, login)
	if admin.cookie == registered.cookie || admin.view.User.ID != registered.view.User.ID {
		t.Fatal("login did not create a fresh session")
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	profileRequest := catalogProfileRequest(t, r, admin)
	config := validIntegrationConfig()
	listener := config["listeners"].([]any)[0].(profiles.Object)
	listener["port"] = port
	listener["banner"] = "hello\n"
	listener["close_after_banner"] = false
	config["logging"] = profiles.Object{"capture_payload": true, "max_payload_bytes": 64}
	config["management"] = profiles.Object{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100}
	profileRequest.Config = config
	profile := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, profileRequest), admin, 201))
	trapRequest := traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Real TCP demo", ProfileID: profile.ID}
	trap := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, trapRequest), admin, 201))
	path := "/api/traps/" + trap.ID
	credentials := decodeIntegration[traps.AgentCredentials](t, sendOperator(t, r, "POST", path+"/agent-credentials", `{"expected_generation":0}`, admin, 200))
	if credentials.AgentWSURL != endpoint || credentials.TrapID != trap.ID || credentials.Token == "" {
		t.Fatal("issued agent credentials do not match the live endpoint")
	}

	executable := filepath.Join(t.TempDir(), "agent")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", executable, "../../cmd/agent")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real agent: %v: %s", err, output)
	}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	journalPath := filepath.Join(t.TempDir(), "agent.journal")
	go func() {
		finished <- agent.Run(ctx, agent.Options{
			URL: credentials.AgentWSURL, Token: credentials.Token, TrapID: credentials.TrapID,
			JournalPath: journalPath, Executable: executable,
			TLS: server.Client().Transport.(*http.Transport).TLSClientConfig,
		})
	}()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Errorf("agent stopped: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("agent did not shut down")
		}
	}()

	createCommand := func(action string, params json.RawMessage) commands.Command {
		request := commands.CreateRequest{RequestID: string(contract.NewID()), Action: action, Params: params}
		return decodeIntegration[commands.Command](t, sendOperator(t, r, "POST", path+"/commands", integrationJSON(t, request), admin, 201))
	}
	waitCommand := func(command commands.Command) {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			current := decodeIntegration[commands.Command](t, sendOperator(t, r, "GET", path+"/commands/"+command.ID, "", admin, 200))
			if current.Status == commands.Succeeded {
				t.Logf("%s command succeeded", command.Action)
				return
			}
			if current.Status == commands.Failed || current.Status == commands.Expired {
				t.Fatalf("%s command ended: status=%s error=%+v", command.Action, current.Status, current.Error)
			}
			select {
			case err := <-finished:
				finished <- err
				t.Fatalf("agent stopped before %s completed: %v", command.Action, err)
			default:
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("%s command did not complete", command.Action)
	}

	waitCommand(createCommand("apply_config", json.RawMessage(`{"profile_revision":1}`)))
	waitCommand(createCommand("start", json.RawMessage(`{}`)))

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	connection, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		t.Fatalf("connect to real TCP trap: %v", err)
	}
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	banner := make([]byte, len("hello\n"))
	if _, err := io.ReadFull(connection, banner); err != nil || string(banner) != "hello\n" {
		t.Fatalf("TCP banner %q: %v", banner, err)
	}
	attack := []byte("attack-probe")
	if _, err := connection.Write(attack); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}

	var received []events.EventSummary
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?trap_id="+trap.ID, "", admin, 200))
		received = page.Items
		if len(received) >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(received) < 3 {
		t.Fatalf("only %d TCP events reached Kafka/PostgreSQL", len(received))
	}
	kinds := make(map[string]bool)
	for _, summary := range received {
		kinds[summary.EventType] = true
		if summary.EventType == "tcp.payload_received" {
			detail := decodeIntegration[events.Event](t, sendOperator(t, r, "GET", "/api/events/"+summary.EventID, "", admin, 200))
			var data struct {
				Payload string `json:"payload_base64"`
			}
			if err := json.Unmarshal(detail.Data, &data); err != nil {
				t.Fatal(err)
			}
			payload, err := base64.StdEncoding.DecodeString(data.Payload)
			if err != nil || string(payload) != string(attack) {
				t.Fatalf("captured payload %q: %v", payload, err)
			}
		}
	}
	for _, kind := range []string{"tcp.connection_opened", "tcp.payload_received", "tcp.connection_closed"} {
		if !kinds[kind] {
			t.Fatalf("missing %s from persisted events", kind)
		}
	}
	t.Logf("real TCP attack produced %d persisted events", len(received))

	waitCommand(createCommand("stop", json.RawMessage(`{}`)))
	stopped := decodeIntegration[traps.Trap](t, sendOperator(t, r, "GET", path, "", admin, 200))
	if stopped.RuntimeState != "stopped" || stopped.DesiredState != "stopped" {
		t.Fatalf("trap state after stop: runtime=%s desired=%s", stopped.RuntimeState, stopped.DesiredState)
	}
	connection, err = net.DialTimeout("tcp", address, time.Second)
	if err == nil {
		connection.Close()
		t.Fatal("TCP listener still accepts connections after stop")
	}
}
