package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStdioWorkerHelper(t *testing.T) {
	if os.Getenv("HF_STDIO_HELPER") != "1" {
		return
	}
	if err := RunTrap(context.Background()); err != nil {
		if _, writeErr := io.WriteString(os.Stderr, err.Error()+"\n"); writeErr != nil {
			os.Exit(1)
		}
	}
	os.Exit(0) // The test harness must not write "PASS" into the TLS stream.
}

func TestStdioWorkerClosedOutput(t *testing.T) {
	_, cert, err := newLocalTLS()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStdioWorkerHelper$")
	cmd.Env = append(os.Environ(), "HF_STDIO_HELPER=1", "NM_STDIO=1")
	var bootstrap bytes.Buffer
	if err := json.NewEncoder(&bootstrap).Encode(childBootstrap{
		URL: "wss://127.0.0.1/trap-stream", Token: "private-local-token",
		Certificate: cert, Snapshot: demoSnapshot(),
	}); err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = &bootstrap
	output, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	// FD 1 has no reader before the worker's first TLS write. This forces the
	// broken-pipe path without depending on a race with the parent's shutdown.
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = writer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("worker did not return after losing its parent: %v; context=%v; stderr=%s", err, ctx.Err(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "connect trap to agent") {
		t.Fatalf("worker did not report a TLS connection error: %s", stderr.String())
	}
}

func TestStdioWorkerTelemetryAndParentDisconnect(t *testing.T) {
	for _, tt := range []struct {
		name, token string
		accepted    bool
	}{
		{"authenticated TLS pipe", "private-local-token", true},
		{"wrong local credential", "wrong", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
			if err := probe.Close(); err != nil {
				t.Fatal(err)
			}
			snapshot := demoSnapshot()
			snapshot.Config["listeners"].([]any)[0].(map[string]any)["port"] = port
			local.Configure(snapshot)
			config, cert, err := newLocalTLS()
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewUnstartedServer(local.Handler(t.Context()))
			server.TLS = config
			server.StartTLS()
			defer server.Close()
			p := &Process{URL: "wss" + strings.TrimPrefix(server.URL, "https") + "/trap-stream"}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestStdioWorkerHelper$")
			cmd.Env = append(os.Environ(), "HF_STDIO_HELPER=1", "NM_STDIO=1")
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			cmd.Stdout = writer
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			if err := json.NewEncoder(stdin).Encode(childBootstrap{URL: p.URL, Token: tt.token, Certificate: cert, Snapshot: snapshot}); err != nil {
				t.Fatal(err)
			}
			if err := p.startBridge(t.Context(), output, stdin); err != nil {
				t.Fatal(err)
			}
			defer p.bridge.Close()
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			if tt.accepted {
				select {
				case <-local.Ready:
				case <-time.After(5 * time.Second):
					t.Fatal("worker did not become ready")
				}
				conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				banner, err := io.ReadAll(conn)
				conn.Close()
				if err != nil || string(banner) != "hello" {
					t.Fatalf("banner=%q err=%v", banner, err)
				}
				p.bridge.Close()
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("helper: %v %s", err, stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("worker survived losing its parent")
			}
			if tt.accepted && len(journal.Pending()) == 0 {
				t.Fatal("worker events did not cross the TLS pipe")
			}
			if !tt.accepted {
				if len(journal.Pending()) != 0 {
					t.Fatal("unauthenticated worker wrote telemetry")
				}
				if !strings.Contains(stderr.String(), "connect trap to agent") {
					t.Fatalf("worker failed outside authentication: %s", stderr.String())
				}
			}
		})
	}
}
