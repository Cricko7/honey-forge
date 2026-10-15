package pythonhttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestEmbeddedTrapsOverHTTP(t *testing.T) {
	for _, kind := range []string{"redis", "honeytoken"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "redis" {
				if _, err := exec.LookPath("redis-server"); err != nil {
					t.Skip("redis-server is required")
				}
			}

			probe, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := probe.Addr().(*net.TCPAddr).Port
			if err := probe.Close(); err != nil {
				t.Fatal(err)
			}

			config := map[string]any{"key": "result", "commands": "\"${json}\""}
			if kind == "honeytoken" {
				config = map[string]any{"key": "result", "path": "${path}", "tokens": `[{"id":"key","kind":"key","path":"/secret","value":"bait-key-0123456789"}]`}
			}

			snapshot := profiles.Snapshot{TypeID: "custom-http", TypeVersion: 3, ProfileRevision: 1, Config: profiles.Object{
				"services": []any{map[string]any{"name": "web", "port": port, "entry": "web"}},
				"blocks": []any{
					map[string]any{"id": "web", "kind": "input", "config": map[string]any{"name": "web", "port": port, "method": "ANY", "path": "*"}, "outputs": map[string]any{"out": "parse"}},
					map[string]any{"id": "parse", "kind": "parse", "config": map[string]any{"format": "auto"}, "outputs": map[string]any{"out": "bait"}},
					map[string]any{"id": "bait", "kind": kind, "config": config, "outputs": map[string]any{"out": "reply", "error": "bad"}},
					map[string]any{"id": "reply", "kind": "response", "config": map[string]any{"format": "json", "status": 200, "body": `{"status":"${vars.result_status}","body":"${vars.result}"}`, "headers": "{}"}, "outputs": map[string]any{}},
					map[string]any{"id": "bad", "kind": "response", "config": map[string]any{"format": "json", "status": 503, "body": `{"error":"unavailable"}`, "headers": "{}"}, "outputs": map[string]any{}},
				},
				"limits":     map[string]any{"max_steps": 512, "timeout_ms": 10000, "max_body_bytes": 65536},
				"management": map[string]any{"heartbeat_interval_seconds": 10, "telemetry_flush_interval_ms": 500},
			}}

			var mu sync.Mutex
			hits := 0
			runtime, err := Start(t.Context(), snapshot, "127.0.0.1", func(_ context.Context, event events.AgentEvent) error {
				if event.EventType == "http.custom" && strings.Contains(string(event.Data), `"honeytoken.triggered"`) {
					mu.Lock()
					hits++
					mu.Unlock()
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Stop()

			client := &http.Client{Timeout: 15 * time.Second}
			request := func(method, body, key string) string {
				t.Helper()
				req, err := http.NewRequest(method, "http://"+net.JoinHostPort("127.0.0.1", strconv.Itoa(port))+"/secret", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				if key != "" {
					req.Header.Set("Authorization", "Bearer "+key)
				}

				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				raw, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != 200 {
					t.Fatalf("HTTP %d: %s", response.StatusCode, raw)
				}
				return string(raw)
			}

			if kind == "redis" {
				if got := request("POST", `[["SET","name","alice"],["GET","name"]]`, ""); !strings.Contains(got, `["OK","alice"]`) {
					t.Fatal(got)
				}
				if got := request("POST", `[["GET","name"]]`, ""); !strings.Contains(got, `["alice"]`) {
					t.Fatal(got)
				}
				if got := request("POST", `[["UNKNOWN_COMMAND"]]`, ""); !strings.Contains(got, `"error"`) {
					t.Fatal(got)
				}

				replies := make(chan string, 8)
				var concurrent sync.WaitGroup
				for range 8 {
					concurrent.Add(1)
					go func() {
						defer concurrent.Done()
						replies <- request("POST", `[["INCR","visits"]]`, "")
					}()
				}

				concurrent.Wait()
				close(replies)
				unique := map[string]bool{}
				for reply := range replies {
					unique[reply] = true
				}
				if len(unique) != 8 {
					t.Fatalf("concurrent increments lost: %v", unique)
				}
			} else {
				if got := request("GET", `null`, ""); !strings.Contains(got, `"status":401`) {
					t.Fatal(got)
				}
				mu.Lock()
				before := hits
				mu.Unlock()
				if before != 0 {
					t.Fatal("unauthorized token triggered")
				}
				if got := request("GET", `null`, "bait-key-0123456789"); !strings.Contains(got, `"status":200`) {
					t.Fatal(got)
				}
				mu.Lock()
				after := hits
				mu.Unlock()
				if after != 1 {
					t.Fatalf("trigger count=%d", after)
				}
			}
		})
	}
}
