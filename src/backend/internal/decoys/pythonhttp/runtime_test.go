package pythonhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

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
