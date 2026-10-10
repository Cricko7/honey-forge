package honeytrap

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"honey-forge/modules/events"
)

func TestRuntimeConcurrentRequestsAndStop(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot()
	snapshot.Config["services"].([]any)[0].(map[string]any)["port"] = port
	var mu sync.Mutex
	var collected []events.AgentEvent
	r, err := Start(t.Context(), snapshot, "127.0.0.1", func(_ context.Context, e events.AgentEvent) error {
		mu.Lock()
		defer mu.Unlock()
		collected = append(collected, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	client := &http.Client{Timeout: 5 * time.Second}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/backup.txt")
			if err != nil {
				t.Error(err)
				return
			}
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
			if response.StatusCode != 200 {
				t.Errorf("status %d", response.StatusCode)
			}
		})
	}
	wg.Wait()
	r.Stop()
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	mu.Lock()
	defer mu.Unlock()
	sessions := map[string][]events.AgentEvent{}
	for _, e := range collected {
		sessions[e.SessionID] = append(sessions[e.SessionID], e)
	}
	if len(sessions) != 12 {
		t.Fatalf("sessions %d", len(sessions))
	}
	for _, session := range sessions {
		if len(session) != 3 || session[1].EventType != "honeytoken.triggered" || session[2].SessionSequence != 3 {
			t.Fatal(session)
		}
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err == nil {
		connection.Close()
		t.Fatal("listener remained open")
	}
}
