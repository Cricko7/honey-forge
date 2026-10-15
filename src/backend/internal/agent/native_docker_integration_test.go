//go:build integration

package agent

import (
	"bytes"
	"context"
	"encoding/hex"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/profiles"
)

// TEST_CONPOT_DOCKER=1 DECOY_CONPOT_IMAGE=honeyforge/conpot-decoy:local
// go test -tags=integration ./internal/agent -run TestConpotDockerIsolation
func TestConpotDockerIsolation(t *testing.T) {
	testNativeDockerIsolation(t, "conpot", "modbus", "000100000006010100010001")
}
func TestDionaeaDockerIsolation(t *testing.T) {
	testNativeDockerIsolation(t, "dionaea", "mqtt", "c000")
}
func testNativeDockerIsolation(t *testing.T, engine, protocol, probeHex string) {
	if os.Getenv("TEST_"+strings.ToUpper(engine)+"_DOCKER") != "1" {
		t.Skip("Linux Docker and TEST_<ENGINE>_DOCKER=1 required")
	}
	image := os.Getenv("DECOY_" + strings.ToUpper(engine) + "_IMAGE")
	if image == "" {
		t.Fatal("DECOY_<ENGINE>_IMAGE required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	id := string(contract.NewID())
	network := engine + "-test-" + id
	probe := engine + "-probe-" + id
	docker("network", "create", network)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, args := range [][]string{{"rm", "--force", "nm-worker-" + id}, {"rm", "--force", probe}, {"network", "rm", network}} {
			if out, err := exec.CommandContext(cleanup, "docker", args...).CombinedOutput(); err != nil {
				t.Logf("cleanup: %v %s", err, out)
			}
		}
	})
	probeProgram := `import http.server,socket,threading
count=0
udp=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);udp.bind(('0.0.0.0',18080))
def receive():
 global count
 while True: udp.recvfrom(8192);count+=1
threading.Thread(target=receive,daemon=True).start()
class Handler(http.server.BaseHTTPRequestHandler):
 def do_GET(self): self.send_response(200);self.end_headers();self.wfile.write(str(count).encode())
http.server.HTTPServer(('0.0.0.0',18080),Handler).serve_forever()`
	docker("run", "--detach", "--rm", "--name", probe, "--network", network, "--entrypoint", "python3", image, "-I", "-u", "-c", probeProgram)
	peer := docker("inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", probe)
	query := `import urllib.request,sys;print(urllib.request.urlopen('http://'+sys.argv[1]+':18080',timeout=2).read().decode())`
	baseline := docker("run", "--rm", "--network", network, "--entrypoint", "python3", image, "-I", "-c", query, peer)
	if baseline != "0" {
		t.Fatal("probe baseline", baseline)
	}
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), id, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	local, err := NewLocal(journal, "private-local-credential")
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, cert, err := newLocalTLS()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(local.Handler(ctx))
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := profiles.Snapshot{TypeID: engine, TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{
		"services":   []any{map[string]any{"name": "plc", "port": port, "protocol": protocol}},
		"session":    map[string]any{"max_connections": 16, "max_seconds": 300},
		"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 500},
	}}
	p := Process{WorkerImage: image, WorkerNetwork: network, WorkerBindIP: "127.0.0.1", TrapID: id, URL: "wss" + strings.TrimPrefix(server.URL, "https") + "/trap-stream", Certificate: cert, Local: local}
	if err := p.Start(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	verify, err := os.ReadFile("../../../../deploy/verify-isolation.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--interactive", "--network", "none", "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock", "--entrypoint", "/bin/sh", image, "-s", "--", p.workerName())
	cmd.Stdin = bytes.NewReader(verify)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolation: %v %s", err, out)
	}
	out, err := exec.CommandContext(ctx, "docker", "exec", "--user", "65532:65532", p.workerName(), "python3", "-I", "-c", `import socket,sys;socket.create_connection((sys.argv[1],18080),timeout=1)`, peer).CombinedOutput()
	if err == nil {
		t.Fatalf("outbound TCP allowed: %s", out)
	}
	// A local OUTPUT DROP can reject sendto with EPERM instead of silently dropping.
	docker("exec", "--user", "65532:65532", p.workerName(), "python3", "-I", "-c", `import socket,sys
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
try:
 s.sendto(b'test',(sys.argv[1],18080))
except PermissionError:
 pass`, peer)
	if count := docker("run", "--rm", "--network", network, "--entrypoint", "python3", image, "-I", "-c", query, peer); count != "0" {
		t.Fatalf("outbound UDP reached peer: %s", count)
	}
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	request, err := hex.DecodeString(probeHex)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(request); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 256)
	if _, err := conn.Read(response); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, batch := range journal.Pending() {
		for _, e := range batch.Events {
			seen[e.EventType] = true
		}
	}
	for _, kind := range []string{"service.connection_opened", "service.action", "service.connection_closed"} {
		if !seen[kind] {
			t.Errorf("missing %s across TLS-pipe", kind)
		}
	}
}
