//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"honey-forge/internal/contract"
	"honey-forge/modules/events"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestRedisServerCrashRestoresDurableEventsAndResults(t *testing.T) {
	executable := os.Getenv("TEST_REDIS_SERVER_PATH")
	if executable == "" {
		t.Skip("TEST_REDIS_SERVER_PATH required")
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	address := "127.0.0.1:" + strconv.Itoa(port)
	directory := t.TempDir()
	var process *exec.Cmd
	start := func() {
		process = exec.Command(executable, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--dir", directory, "--appendonly", "yes", "--appendfsync", "always", "--maxmemory-policy", "noeviction")
		hideWindow(process)
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
		client := redis.NewClient(&redis.Options{Addr: address})
		defer client.Close()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if client.Ping(t.Context()).Err() == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("Redis did not start")
	}
	stop := func() {
		if process != nil {
			process.Process.Kill()
			process.Wait()
			process = nil
		}
	}
	defer stop()
	start()
	url := "redis://" + address + "/0"
	id := string(contract.NewID())
	j, err := OpenRedisJournal(t.Context(), url, id, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	e := events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), EventType: "tcp.connection_opened", Data: json.RawMessage(`{"listener_name":"demo"}`)}
	batch, err := j.Enqueue(e)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := demoSnapshot()
	snapshot.ProfileRevision = 2
	dispatch := configCommand("apply_config", &snapshot)
	s := NewService(j, &fakeRunner{})
	if _, err := s.Execute(t.Context(), dispatch); err != nil {
		t.Fatal(err)
	}
	stop()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	if err := j.CheckStorage(ctx); err == nil {
		t.Fatal("Redis outage was ignored")
	}
	cancel()
	start()
	if err := j.CheckStorage(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(j.Pending()) != 1 || j.Pending()[0].BatchID != batch.BatchID || j.LoadState().Configuration.ProfileRevision != 2 {
		t.Fatal("Redis restart lost durable state")
	}
	dispatch.LeaseID = string(contract.NewID())
	if result, err := s.Execute(t.Context(), dispatch); err != nil || result.LeaseID != dispatch.LeaseID || *result.Runtime.AppliedProfileRevision != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	if err := j.Ack(batch.BatchID, []string{e.EventID}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
}
