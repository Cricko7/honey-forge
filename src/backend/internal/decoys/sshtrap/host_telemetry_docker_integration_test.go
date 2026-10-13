//go:build integration

package sshtrap

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"honey-forge/modules/events"
)

// The operator must install the privileged sensor separately. This test only
// creates the same isolated sandboxes as an ordinary authorized SSH session.
func TestTetragonDockerExecutions(t *testing.T) {
	if os.Getenv("TEST_TETRAGON_DOCKER") != "1" {
		t.Skip("TEST_TETRAGON_DOCKER=1, Linux Docker, NM_SANDBOX_IMAGE and NM_TETRAGON_CONTAINER required")
	}
	t.Setenv("NM_INSTANCE_ID", "11111111-1111-4111-8111-111111111111")
	t.Setenv("NM_DEPLOYMENT_GENERATION", "1")
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot(port)
	snapshot.TypeVersion = 2
	snapshot.Config["session"].(map[string]any)["max_seconds"] = 60
	snapshot.Config["session"].(map[string]any)["max_connections"] = 2
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
	for range 2 {
		client, err := ssh.Dial("tcp", r.listener.Addr().String(), &ssh.ClientConfig{
			User: "root", Auth: []ssh.AuthMethod{ssh.Password("bait")},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		output, err := session.CombinedOutput("/usr/bin/id -u; /usr/bin/uname -s; /usr/bin/ls /tmp")
		if closeErr := session.Close(); closeErr != nil && err == nil {
			// The peer may already have closed a completed SSH channel.
			t.Log("completed SSH channel:", closeErr)
		}
		if err != nil || !strings.HasPrefix(string(output), "1000\nLinux\n") {
			t.Fatalf("sandbox command: %v %s", err, output)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		containers := map[string]string{}
		programs := map[string]map[string]bool{}
		for _, e := range captured {
			if e.EventType != "process.exec" {
				continue
			}
			var data struct {
				ContainerID string `json:"container_id"`
				Binary      string `json:"binary"`
			}
			if err := json.Unmarshal(e.Data, &data); err != nil {
				mu.Unlock()
				t.Fatal(err)
			}
			if old := containers[data.ContainerID]; old != "" && old != e.SessionID {
				mu.Unlock()
				t.Fatal("one container assigned to two sessions")
			}
			containers[data.ContainerID] = e.SessionID
			if programs[e.SessionID] == nil {
				programs[e.SessionID] = map[string]bool{}
			}
			programs[e.SessionID][data.Binary] = true
		}
		mu.Unlock()
		complete := len(containers) == 2
		for _, binaries := range programs {
			complete = complete && binaries["/usr/bin/id"] && binaries["/usr/bin/uname"] && binaries["/usr/bin/ls"]
		}
		if complete {
			return
		}
		if err := r.Err(); err != nil {
			t.Fatal("sensor failed:", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("real Tetragon did not deliver all three executions for two isolated sessions")
}
