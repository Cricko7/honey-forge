package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"honey-forge/modules/profiles"
)

func TestStdioWorkerProtocolsAfterIdle(t *testing.T) {
	for _, kind := range []string{"redis-emulator", "honeytoken-http"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "redis-emulator" {
				if _, err := exec.LookPath("redis-server"); err != nil {
					t.Skip("redis-server required; run Redis worker coverage in Linux/Docker")
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
			defer cancel()
			journal, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			local, err := NewLocal(journal, "private-local-token")
			if err != nil {
				t.Fatal(err)
			}
			probe, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := probe.Addr().(*net.TCPAddr).Port
			probe.Close()
			snapshot := profiles.Snapshot{TypeID: kind, TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{
				"services":   []any{map[string]any{"name": "trap", "port": port}},
				"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
			}}
			if kind == "redis-emulator" {
				snapshot.Config["services"].([]any)[0].(map[string]any)["password"] = "bait-pass"
			} else {
				snapshot.Config["tokens"] = []any{map[string]any{"id": "document", "kind": "file", "path": "/backup.txt", "value": "fake backup"}}
			}
			local.Configure(snapshot)
			config, cert, err := newLocalTLS()
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewUnstartedServer(local.Handler(ctx))
			server.TLS = config
			server.StartTLS()
			defer server.Close()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStdioWorkerHelper$")
			cmd.Env = append(os.Environ(), "HF_STDIO_HELPER=1", "NM_STDIO=1")
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			cmd.Stdout = writer
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			writer.Close()
			defer cmd.Process.Kill()
			p := &Process{URL: "wss" + strings.TrimPrefix(server.URL, "https") + "/trap-stream"}
			if err := json.NewEncoder(input).Encode(childBootstrap{URL: p.URL, Token: "private-local-token", Certificate: cert, Snapshot: snapshot}); err != nil {
				t.Fatal(err)
			}
			if err := p.startBridge(ctx, output, input); err != nil {
				t.Fatal(err)
			}
			defer p.bridge.Close()
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-local.Ready:
			case err := <-done:
				t.Fatalf("worker exited: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// A real worker must remain usable after handshake/write deadlines elapse.
			select {
			case <-time.After(6 * time.Second):
			case err := <-done:
				t.Fatalf("worker exited during idle: %v", err)
			}
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			for range 2 {
				if kind == "redis-emulator" {
					conn, err := net.DialTimeout("tcp", address, time.Second)
					if err != nil {
						t.Fatal(err)
					}
					conn.SetDeadline(time.Now().Add(3 * time.Second))
					_, err = io.WriteString(conn, "*1\r\n$4\r\nPING\r\n")
					if err != nil {
						conn.Close()
						t.Fatal(err)
					}
					reply, err := bufio.NewReader(conn).ReadString('\n')
					conn.Close()
					if err != nil || (!strings.HasPrefix(reply, "+") && !strings.HasPrefix(reply, "-")) {
						t.Fatalf("Redis reply=%q error=%v", reply, err)
					}
				} else {
					client := &http.Client{Timeout: 3 * time.Second}
					response, err := client.Get("http://" + address + "/backup.txt")
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(response.Body)
					response.Body.Close()
					client.CloseIdleConnections()
					if err != nil || response.StatusCode != 200 || string(body) != "fake backup" {
						t.Fatalf("HTTP status=%d body=%q error=%v", response.StatusCode, body, err)
					}
				}
			}
			if len(journal.Pending()) == 0 {
				t.Fatal("worker answered without committing events")
			}
			if err := p.bridge.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(fmt.Errorf("helper: %w", err))
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
