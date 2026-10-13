//go:build integration

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"honey-forge/internal/contract"
	"honey-forge/modules/profiles"
)

// Exercise actual Docker networking, worker privilege drop, TLS pipe transport,
// port publishing, event delivery, restart and graceful shutdown together.
func TestDockerWorkerMASK3Isolation(t *testing.T) {
	testDockerWorkerIsolation(t, false)
}

func TestRealRedisDockerIsolationAndTelemetry(t *testing.T) {
	testDockerWorkerIsolation(t, true)
}

func testDockerWorkerIsolation(t *testing.T, realRedis bool) {
	if os.Getenv("TEST_MASK3_DOCKER") != "1" {
		t.Skip("TEST_MASK3_DOCKER=1 and a built DECOY_TCP_IMAGE required")
	}

	image := os.Getenv("DECOY_TCP_IMAGE")
	if realRedis {
		image = os.Getenv("DECOY_REDIS_IMAGE")
	}

	if image == "" {
		t.Fatal("the selected DECOY_TCP_IMAGE or DECOY_REDIS_IMAGE is required")
	}

	id := string(contract.NewID())
	network, probeName := "mask3-"+id, "mask3-probe-"+id
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()

		output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("Docker %v: %v %s", args, err, output)
		}

		return strings.TrimSpace(string(output))
	}
	docker("network", "create", network)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, args := range [][]string{{"rm", "--force", "nm-worker-" + id}, {"rm", "--force", probeName}, {"network", "rm", network}} {
			if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil && !strings.Contains(string(output), "No such container") {
				t.Errorf("cleanup: %v %s", err, output)
			}
		}
	})
	// This synthetic peer is intentionally reachable within the decoy subnet;
	// the worker must still be unable to initiate a connection to it.
	docker("run", "--detach", "--rm", "--name", probeName, "--network", network, "--network-alias", "probe", "redis:7.4.6", "redis-server", "--protected-mode", "no")
	for deadline := time.Now().Add(10 * time.Second); ; {
		output, err := exec.CommandContext(t.Context(), "docker", "exec", probeName, "redis-cli", "ping").CombinedOutput()
		if err == nil && strings.TrimSpace(string(output)) == "PONG" {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("probe not ready: %v %s", err, output)
		}

		time.Sleep(100 * time.Millisecond)
	}

	baseline := docker("run", "--rm", "--network", network, "--entrypoint", "/bin/sh", image, "-c", `printf 'PING\r\n' | nc -w 1 probe 6379`)
	if !strings.Contains(baseline, "PONG") {
		t.Fatalf("probe is not reachable for the baseline client: %s", baseline)
	}

	journal, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), id, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	defer journal.Close()

	local, err := NewLocal(journal, "local-test-credential")
	if err != nil {
		t.Fatal(err)
	}

	config, cert, err := newLocalTLS()
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(local.Handler(t.Context()))
	server.TLS = config
	server.StartTLS()
	defer server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	snapshot := demoSnapshot()
	snapshot.Config["listeners"].([]any)[0].(map[string]any)["port"] = port
	if realRedis {
		snapshot = profiles.Snapshot{TypeID: "redis-emulator", TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{
			"services":   []any{map[string]any{"name": "redis", "port": port, "password": "bait-pass"}},
			"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
		}}
	}

	p := &Process{
		WorkerImage:   image,
		WorkerNetwork: network,
		WorkerBindIP:  "127.0.0.1",
		TrapID:        id,
		URL:           "wss" + strings.TrimPrefix(server.URL, "https") + "/trap-stream",
		Certificate:   cert,
		Local:         local,
	}
	defer p.Stop()

	for cycle := 0; cycle < 2; cycle++ {
		if err := p.Start(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}

		if user := docker("exec", "--user", "65532:65532", p.workerName(), "/bin/sh", "-c", `awk '/^Uid:/ {print $2}' /proc/$(pidof node-metrics)/status`); user != "65532" {
			t.Fatalf("worker retained root: %s", user)
		}

		output, err := exec.CommandContext(t.Context(), "docker", "exec", "--user", "65532:65532", p.workerName(), "/bin/sh", "-c", `timeout 2 nslookup probe 127.0.0.11`).CombinedOutput()
		if err == nil {
			t.Fatalf("worker can query Docker DNS: %s", output)
		}

		peerIP := docker("inspect", "--format", `{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}`, probeName)
		output, err = exec.CommandContext(t.Context(), "docker", "exec", "--user", "65532:65532", p.workerName(), "/bin/sh", "-c", `printf 'PING\r\n' | nc -w 1 "$1" 6379`, "probe", peerIP).CombinedOutput()
		if err == nil || strings.Contains(string(output), "PONG") {
			t.Fatalf("worker opened an outbound connection: %v %s", err, output)
		}

		if _, err := exec.CommandContext(t.Context(), "docker", "exec", "--user", "65532:65532", p.workerName(), "iptables", "-P", "OUTPUT", "ACCEPT").CombinedOutput(); err == nil {
			t.Fatal("worker can remove its egress firewall")
		}

		if cycle == 0 {
			script, err := os.ReadFile("../../../../deploy/verify-isolation.sh")
			if err != nil {
				t.Fatal(err)
			}

			verify := func() ([]byte, error) {
				cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm", "--interactive", "--network", "none",
					"--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
					"--entrypoint", "/bin/sh", image, "-s", "--", p.workerName())
				cmd.Stdin = bytes.NewReader(script)

				return cmd.CombinedOutput()
			}

			if output, err := verify(); err != nil || !strings.Contains(string(output), "PASS: service worker") {
				t.Fatalf("isolation verifier did not inspect worker: %v %s", err, output)
			}

			docker("exec", "--user", "0:0", p.workerName(), "iptables", "-A", "OUTPUT", "-o", "lo", "-j", "ACCEPT")
			output, err := verify()

			docker("exec", "--user", "0:0", p.workerName(), "iptables", "-D", "OUTPUT", "-o", "lo", "-j", "ACCEPT")
			if err == nil || !strings.Contains(string(output), "FAIL: Worker permits") {
				t.Fatalf("verifier accepted an open loopback channel: %v %s", err, output)
			}
		}

		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		if realRedis {
			testRealRedisClient(t, address)
			if err := p.Stop(); err != nil {
				t.Fatal(err)
			}

			continue
		}

		conn, err := net.DialTimeout("tcp", address, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}

		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}

		buffer := make([]byte, 5)
		n, err := conn.Read(buffer)
		conn.Close()
		if err != nil || string(buffer[:n]) != "hello" {
			t.Fatalf("decoy banner: %q %v", buffer[:n], err)
		}

		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
	}

	if len(journal.Pending()) < 2 {
		t.Fatal("Docker worker lost telemetry")
	}

	if realRedis {
		seen := map[string]bool{}
		largeCommandObserved := false
		for _, batch := range journal.Pending() {
			for _, event := range batch.Events {
				seen[event.EventType] = true
				if event.EventType == "service.action" {
					var data struct {
						Input         string `json:"input"`
						Truncated     bool   `json:"truncated"`
						ReceivedBytes int    `json:"received_bytes"`
					}
					if err := json.Unmarshal(event.Data, &data); err != nil {
						t.Fatal(err)
					}

					if strings.HasPrefix(data.Input, "set binary:smoke ") || strings.HasPrefix(data.Input, "SET binary:smoke ") {
						largeCommandObserved = data.Truncated && data.ReceivedBytes == 8192 && len(data.Input) <= 4096
					}
				}
			}
		}

		if !largeCommandObserved {
			t.Fatal("large command was not safely clipped in telemetry")
		}

		for _, kind := range []string{"service.connection_opened", "service.auth_attempt", "service.action", "service.connection_closed"} {
			if !seen[kind] {
				t.Fatalf("Redis telemetry did not reach the journal: %s", kind)
			}
		}
	}
}

func testRealRedisClient(t *testing.T, address string) {
	t.Helper()

	client := redis.NewClient(&redis.Options{Addr: address, Password: "bait-pass", DialTimeout: 2 * time.Second})
	defer client.Close()

	if n, err := client.Exists(t.Context(), "user:1001", "product:1001", "orders:recent").Result(); err != nil || n != 3 {
		t.Fatalf("missing synthetic data: %d %v", n, err)
	}

	if err := client.HSet(t.Context(), "cart:smoke", "product:1001", "2").Err(); err != nil {
		t.Fatal(err)
	}

	other := redis.NewClient(&redis.Options{Addr: address, Password: "bait-pass"})
	defer other.Close()

	if value, err := other.HGet(t.Context(), "cart:smoke", "product:1001").Result(); err != nil || value != "2" {
		t.Fatalf("Redis keyspace is not shared: %q %v", value, err)
	}

	value := strings.Repeat("a\x00b\xff", 8192)
	if err := client.Set(t.Context(), "binary:smoke", value, 0).Err(); err != nil {
		t.Fatal(err)
	}

	if got, err := other.Get(t.Context(), "binary:smoke").Result(); err != nil || got != value {
		t.Fatalf("binary data corrupted: %v", err)
	}

	if result, err := client.Eval(t.Context(), "return 40+2", nil).Int(); err != nil || result != 42 {
		t.Fatalf("Lua not executed by Redis: %d %v", result, err)
	}

	sub := other.Subscribe(t.Context(), "smoke:events")
	defer sub.Close()

	if _, err := sub.Receive(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := client.Publish(t.Context(), "smoke:events", "order:created").Err(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	if message, err := sub.ReceiveMessage(ctx); err != nil || message.Payload != "order:created" {
		t.Fatalf("Pub/Sub failed: %v %v", message, err)
	}
}

func TestDockerWorkerInitCanSignalDroppedUser(t *testing.T) {
	if os.Getenv("TEST_MASK3_DOCKER") != "1" {
		t.Skip("TEST_MASK3_DOCKER=1 and a built DECOY_TCP_IMAGE required")
	}
	p := &Process{WorkerImage: os.Getenv("DECOY_TCP_IMAGE"), WorkerNetwork: "bridge", TrapID: string(contract.NewID())}
	args, err := p.workerArgs(t.Context(), demoSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	// Keep the container after exit so the actual init exit code can be inspected.
	fixture := []string{}
	for _, arg := range args[:len(args)-2] {
		if arg != "--rm" && arg != "--interactive" {
			fixture = append(fixture, arg)
		}
	}
	fixture = append(fixture, "--detach", "--entrypoint", "/bin/sh", p.WorkerImage, "-c", "exec su-exec 65532:65532 sleep 60")
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("Docker %v: %v %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "docker", "rm", "--force", p.workerName()).CombinedOutput(); err != nil {
			t.Errorf("cleanup worker: %v %s", err, output)
		}
	})
	docker(fixture...)
	state := docker("exec", "--user", "65532:65532", p.workerName(), "/bin/sh", "-c", `awk '/^Uid:/ {print $2} /^CapEff:/ {print $2}' /proc/$(pidof sleep)/status`)
	if state != "65532\n0000000000000000" {
		t.Fatalf("child retained privileges: %s", state)
	}
	docker("stop", "--time", "3", p.workerName())
	if code := docker("inspect", "--format", "{{.State.ExitCode}}", p.workerName()); code != "143" {
		t.Fatalf("init did not forward SIGTERM: exit=%s logs=%s", code, docker("logs", p.workerName()))
	}
}
