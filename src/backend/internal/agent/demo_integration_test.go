//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
)

// Real child process, TCP listener, local WS, backend WSS and durable queue.
// The backend is a protocol peer; PostgreSQL is tested separately by app tests.
func TestDemoTrapToBackend(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "agent")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", executable, "../../cmd/agent")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build agent: %s: %v", output, err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	snapshot := demoSnapshot()
	snapshot.Config["listeners"].([]any)[0].(map[string]any)["port"] = port
	snapshot.Config["listeners"].([]any)[0].(map[string]any)["close_after_banner"] = false
	snapshot.Config["logging"] = map[string]any{"capture_payload": true, "max_payload_bytes": 3}
	trapID := string(contract.NewID())
	commandsReady := make(chan struct{})
	captured := make(chan Batch, 4)
	serverErrors := make(chan error, 1)
	router := gin.New()
	router.GET("/assets/stream", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer demo-token" {
			c.Status(401)
			return
		}
		conn, err := (&websocket.Upgrader{Subprotocols: []string{"resource-stream.v1"}}).Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.Close()
		send := func(kind string, reply *contract.ID, body any) error {
			raw, err := json.Marshal(body)
			if err != nil {
				return err
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				return err
			}
			return conn.WriteJSON(contract.Envelope{MessageID: contract.NewID(), Type: kind, ReplyTo: reply, Payload: fields})
		}
		conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		var hello contract.Envelope
		if err := conn.ReadJSON(&hello); err != nil {
			serverErrors <- err
			return
		}
		if err := send("agent.welcome", &hello.MessageID, map[string]any{"trap_id": trapID, "heartbeat_interval_seconds": 5}); err != nil {
			serverErrors <- err
			return
		}
		dispatch := func(action string) error {
			d := commands.Dispatch{CommandID: string(contract.NewID()), LeaseID: string(contract.NewID()), Action: action, ExpiresAt: time.Now().Add(time.Minute), Params: json.RawMessage(`{}`)}
			if action == "apply_config" {
				d.Configuration = &snapshot
			}
			return send("command.dispatch", nil, d)
		}
		if err := dispatch("apply_config"); err != nil {
			serverErrors <- err
			return
		}
		configured := false
		for {
			var m contract.Envelope
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			switch m.Type {
			case "command.progress":
				if err := send("command.progress_ack", &m.MessageID, map[string]any{}); err != nil {
					return
				}
			case "command.result":
				var result commands.AgentResult
				if err := payload(m, &result); err != nil {
					serverErrors <- err
					return
				}
				if result.Status != commands.Succeeded {
					serverErrors <- fmtResultError(result)
					return
				}
				if err := send("command.ack", &m.MessageID, map[string]any{}); err != nil {
					return
				}
				if !configured {
					configured = true
					if err := dispatch("start"); err != nil {
						return
					}
				} else {
					select {
					case <-commandsReady:
					default:
						close(commandsReady)
					}
				}
			case "telemetry.batch":
				var b Batch
				if err := payload(m, &b); err != nil {
					serverErrors <- err
					return
				}
				captured <- b
				if err := send("telemetry.ack", &m.MessageID, map[string]any{"batch_id": b.BatchID, "acknowledged_event_ids": []string{b.Events[0].EventID}, "stored_at": time.Now().UTC()}); err != nil {
					return
				}
			case "agent.heartbeat":
				if err := send("agent.heartbeat_ack", &m.MessageID, map[string]any{}); err != nil {
					return
				}
			}
		}
	})
	server := httptest.NewTLSServer(router)
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	journal := filepath.Join(t.TempDir(), "journal")
	finished := make(chan error, 1)
	go func() {
		finished <- Run(ctx, Options{URL: "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream", Token: "demo-token", TrapID: trapID, JournalPath: journal, Executable: executable, TLS: server.Client().Transport.(*http.Transport).TLSClientConfig})
	}()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("agent failed to shut down")
		}
	}()
	select {
	case <-commandsReady:
	case err := <-serverErrors:
		t.Fatal(err)
	case <-time.After(20 * time.Second):
		t.Fatal("trap did not start")
	}
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	banner := make([]byte, 5)
	if _, err := io.ReadFull(conn, banner); err != nil || string(banner) != "hello" {
		t.Fatalf("banner %q: %v", banner, err)
	}
	if _, err := conn.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	for _, kind := range []string{"tcp.connection_opened", "tcp.payload_received", "tcp.connection_closed"} {
		select {
		case b := <-captured:
			if b.Events[0].EventType != kind {
				t.Fatal(b)
			}
		case err := <-serverErrors:
			t.Fatal(err)
		case <-time.After(10 * time.Second):
			t.Fatalf("missing %s", kind)
		}
	}
}

func fmtResultError(result commands.AgentResult) error {
	return fmt.Errorf("command failed: %v", result.Error)
}
