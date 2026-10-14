package tcptrap

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestCustomTCPResponsesAndEvents(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := profiles.Snapshot{TypeID: "custom-tcp", TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{
		"listeners": []any{map[string]any{"name": "mail", "port": port, "banner": "220 custom mail\r\n", "close_after_banner": false,
			"rules": []any{map[string]any{"match": "prefix", "input": "HELO", "response": "250 hello\r\n", "close": false},
				map[string]any{"match": "exact", "input": "QUIT", "response": "221 bye\r\n", "close": true}}}},
		"logging":    map[string]any{"capture_payload": true, "max_payload_bytes": 4096},
		"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
	}}
	collected := make(chan events.AgentEvent, 10)
	runtime, err := Start(t.Context(), snapshot, "127.0.0.1", func(_ context.Context, event events.AgentEvent) error {
		collected <- event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Stop)
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", fmtPort(port)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	banner := make([]byte, len("220 custom mail\r\n"))
	if _, err := io.ReadFull(conn, banner); err != nil || string(banner) != "220 custom mail\r\n" {
		t.Fatalf("banner %q: %v", banner, err)
	}
	// TCP can fragment one request and combine multiple requests in a read.
	for _, fragment := range []string{"HE", "LO example\r\nQUIT\r\n"} {
		if _, err := io.WriteString(conn, fragment); err != nil {
			t.Fatal(err)
		}
	}
	response, err := io.ReadAll(conn)
	if err != nil || string(response) != "250 hello\r\n221 bye\r\n" {
		t.Fatalf("response %q: %v", response, err)
	}
	for {
		select {
		case event := <-collected:
			if event.TypeID != "custom-tcp" || !strings.HasPrefix(event.EventType, "tcp.") {
				t.Fatalf("wrong telemetry: %+v", event)
			}
			if event.EventType == "tcp.connection_closed" {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing close event")
		}
	}
}
