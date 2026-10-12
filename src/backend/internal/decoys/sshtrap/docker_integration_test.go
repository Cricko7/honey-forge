//go:build integration

package sshtrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"honey-forge/modules/events"
)

func TestDockerRealLinuxShellSFTPAndIsolation(t *testing.T) {
	if os.Getenv("TEST_SSH_DOCKER") != "1" {
		t.Skip("TEST_SSH_DOCKER=1 and a built NM_SANDBOX_IMAGE are required")
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	snapshot := testSnapshot(port)
	snapshot.Config["session"].(map[string]any)["max_seconds"] = 60
	var mu sync.Mutex
	var captured []events.AgentEvent
	r, err := Start(t.Context(), snapshot, "127.0.0.1", func(_ context.Context, e events.AgentEvent) error {
		mu.Lock()
		defer mu.Unlock()

		captured = append(captured, e)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(r.Stop)
	client, err := ssh.Dial("tcp", r.listener.Addr().String(), &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.Password("bait")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	defer client.Close()

	run := func(command string) []byte {
		s, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}

		defer s.Close()

		output, err := s.CombinedOutput(command)
		if err != nil {
			t.Fatalf("shell command failed: %v: %s", err, output)
		}

		return output
	}
	output := run("uname -s; id -u; test ! -e /var/run/docker.sock && test ! -e /run/secrets/agent_token")
	if string(output) != "Linux\n1000\n" {
		t.Fatalf("sandbox isolation: %s", output)
	}

	// Docker Desktop may create inactive tunnel devices even with --network none.
	run(`test -z "$(tail -n +2 /proc/net/route)" || exit 1
	for interface in /sys/class/net/*; do
		test -f "$interface/flags" || continue
		flags=$(cat "$interface/flags")
		if test "$((flags & 1))" -ne 0 && test "${interface##*/}" != lo; then exit 1; fi
	done`)
	remote, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}

	defer remote.Close()

	f, err := remote.Create("/home/sandbox/demo.bin")
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte{0, 255, 1, 2, 3}
	if _, err := f.Write(payload); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if output := run("cat /home/sandbox/demo.bin"); !bytes.Equal(output, payload) {
		t.Fatalf("real file differs: %x", output)
	}

	if err := remote.Remove("/home/sandbox/demo.bin"); err != nil {
		t.Fatal(err)
	}

	remote.Close()
	client.Close()
	r.Stop()
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(captured) == 0 {
		t.Fatal("no recording")
	}

	var complete bool
	for _, e := range captured {
		if e.EventType == "service.file_closed" {
			var d struct {
				Complete bool `json:"complete"`
			}
			json.Unmarshal(e.Data, &d)
			complete = d.Complete
		}
	}

	if !complete {
		t.Fatal("upload was not recorded")
	}

	output, err = exec.CommandContext(t.Context(), "docker", "ps", "--all", "--filter", "name=node-metrics-session-"+captured[0].SessionID, "--format", "{{.Names}}").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatalf("sandbox was not removed: %s %v", output, err)
	}
}
