package pythonhttp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestPythonHTTPManagedRuntime(t *testing.T) {
	executable := os.Getenv("HF_PYTHON")
	if executable == "" {
		executable = "python3"
	}
	if _, err := exec.LookPath(executable); err != nil {
		t.Skip("python3 is required")
	}
	snapshot := testSnapshot(t)
	received := make(chan events.AgentEvent, 32)
	runtime, err := Start(t.Context(), snapshot, "127.0.0.1", func(_ context.Context, event events.AgentEvent) error {
		received <- event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Stop()
	response, err := http.Post("http://127.0.0.1:32189/api", "application/json", strings.NewReader(`{"user":"Alice"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 201 || body["user"] != "Alice" || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("response: %d %+v", response.StatusCode, body)
	}
	for i := 0; i < 10; i++ {
		event := <-received
		if event.TypeID != "custom-http" || event.TypeVersion != int64(snapshot.TypeVersion) || event.SessionSequence != int64(i+1) {
			t.Fatalf("event: %+v", event)
		}
		if event.EventType == "http.response" {
			return
		}
	}
	t.Fatal("missing response event")
}

func TestPythonHTTPBoundsEscapedRequestTelemetry(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "ascii", path: "/" + strings.Repeat("a", 2047)},
		{name: "escaped bytes", path: "/" + strings.Repeat("\xff", 2047)},
		{name: "HTML sensitive bytes", path: "/" + strings.Repeat("<", 2047)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := probe.Addr().String()
			port := probe.Addr().(*net.TCPAddr).Port
			if err := probe.Close(); err != nil {
				t.Fatal(err)
			}
			snapshot := testSnapshot(t)
			snapshot.Config["services"].([]any)[0].(map[string]any)["port"] = port
			snapshot.Config["blocks"].([]any)[0].(map[string]any)["config"].(map[string]any)["port"] = port
			captured := make(chan events.AgentEvent, 32)
			runtime, err := Start(t.Context(), snapshot, "127.0.0.1", func(_ context.Context, event events.AgentEvent) error {
				raw, err := json.Marshal(event)
				if err != nil {
					return err
				}
				// The local agent rejects WebSocket messages above this limit.
				if len(raw) > 16*1024 {
					return fmt.Errorf("telemetry exceeds local frame limit: %d bytes", len(raw))
				}
				captured <- event
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(runtime.Stop)
			conn, err := net.DialTimeout("tcp", address, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			body := strings.Repeat("x", 4096)
			if _, err := fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\n\r\n%s", tc.path, len(body), body); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatalf("request must keep runtime alive: %v (runtime: %v)", err, runtime.Err())
			}
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", response.StatusCode)
			}
			client := &http.Client{Timeout: 5 * time.Second}
			next, err := client.Post("http://"+address+"/api", "application/json", strings.NewReader(`{"user":"Alice"}`))
			if err != nil {
				t.Fatalf("subsequent request failed: %v", err)
			}
			if _, err := io.Copy(io.Discard, next.Body); err != nil {
				t.Fatal(err)
			}
			if err := next.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if next.StatusCode != http.StatusCreated {
				t.Fatalf("subsequent status = %d, want 201", next.StatusCode)
			}
			for {
				select {
				case event := <-captured:
					if event.EventType == "http.request" {
						var data struct {
							CapturedBytes int `json:"captured_bytes"`
						}
						if err := json.Unmarshal(event.Data, &data); err != nil {
							t.Fatal(err)
						}
						if data.CapturedBytes != len(body) {
							t.Fatalf("captured bytes = %d, want %d", data.CapturedBytes, len(body))
						}
						return
					}
				default:
					t.Fatal("request telemetry missing")
				}
			}
		})
	}
}

func testSnapshot(t *testing.T) profiles.Snapshot {
	t.Helper()
	raw := `{"services":[{"name":"web","port":32189,"entry":"web"}],"blocks":[{"id":"web","kind":"input","config":{"name":"web","port":32189,"method":"POST","path":"/api"},"outputs":{"out":"parse"}},{"id":"parse","kind":"parse","config":{"format":"json"},"outputs":{"out":"reply"}},{"id":"reply","kind":"response","config":{"status":201,"format":"json","body":"{\"user\":\"${json.user}\"}","headers":"{}"},"outputs":{}}],"limits":{"max_steps":512,"timeout_ms":10000,"max_body_bytes":65536},"management":{"heartbeat_interval_seconds":10,"telemetry_flush_interval_ms":500}}`
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	return profiles.Snapshot{TypeID: "custom-http", TypeVersion: 1, ProfileRevision: 1, Config: config}
}

func TestPythonHTTPDescriptor(t *testing.T) {
	snapshot := testSnapshot(t)
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CheckConfig(t.Context(), "custom-http", 1, raw); err != nil {
		t.Fatal(err)
	}
	ports, err := Ports(t.Context(), snapshot)
	if err != nil || len(ports) != 1 || ports[0] != 32189 {
		t.Fatalf("ports %v: %v", ports, err)
	}
}

func TestPythonHTTPLegacyPipelinePorts(t *testing.T) {
	snapshot := testSnapshot(t)
	snapshot.Config["blocks"] = []any{
		map[string]any{"id": "web", "kind": "input", "config": map[string]any{"name": "web", "port": 32189, "method": "POST", "path": "/api"}, "outputs": map[string]any{"out": "event"}},
		map[string]any{"id": "event", "kind": "event", "config": map[string]any{"name": "stage", "severity": "high", "payload": "{}"}, "outputs": map[string]any{"out": "reply"}},
		map[string]any{"id": "reply", "kind": "response", "config": map[string]any{"status": 200, "format": "json", "body": "{}", "headers": "{}"}, "outputs": map[string]any{}},
	}
	ports, err := Ports(t.Context(), snapshot)
	if err != nil || len(ports) != 1 || ports[0] != 32189 {
		t.Fatalf("legacy pipeline ports %v: %v", ports, err)
	}
}
