package tcptrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"testing"
	"time"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestRuntime(t *testing.T) {
	for _, closeBanner := range []bool{true, false} {
		t.Run(map[bool]string{true: "banner closes", false: "captures payload"}[closeBanner], func(t *testing.T) {
			probe, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := probe.Addr().(*net.TCPAddr).Port
			probe.Close()
			snapshot := profiles.Snapshot{ProfileRevision: 1, TypeID: "tcp-banner", TypeVersion: 1, Config: map[string]any{
				"listeners":  []any{map[string]any{"name": "demo", "port": port, "banner": "hello", "close_after_banner": closeBanner}},
				"logging":    map[string]any{"capture_payload": true, "max_payload_bytes": 3},
				"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
			}}
			collected := make(chan events.AgentEvent, 10)
			runtime, err := Start(t.Context(), snapshot, "127.0.0.1", func(ctx context.Context, e events.AgentEvent) error { collected <- e; return nil })
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Stop()
			conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", fmtPort(port)))
			if err != nil {
				t.Fatal(err)
			}
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			banner := make([]byte, 5)
			if _, err := conn.Read(banner); err != nil || string(banner) != "hello" {
				t.Fatalf("banner %q: %v", banner, err)
			}
			if !closeBanner {
				if _, err := conn.Write([]byte("abcdef")); err != nil {
					t.Fatal(err)
				}
			}
			conn.Close()
			var got []events.AgentEvent
			for {
				select {
				case e := <-collected:
					got = append(got, e)
					if e.EventType == "tcp.connection_closed" {
						goto done
					}
				case <-time.After(3 * time.Second):
					t.Fatal("missing close")
				}
			}
		done:
			if got[0].EventType != "tcp.connection_opened" {
				t.Fatal(got)
			}
			for i, e := range got {
				if e.SessionSequence != int64(i+1) || e.SessionID != got[0].SessionID || e.ProfileRevision != 1 {
					t.Fatal(e)
				}
			}
			if !closeBanner {
				var data struct {
					Payload   string `json:"payload_base64"`
					Truncated bool   `json:"truncated"`
					Original  int    `json:"original_bytes"`
				}
				if len(got) != 3 {
					t.Fatal(got)
				}
				if err := json.Unmarshal(got[1].Data, &data); err != nil {
					t.Fatal(err)
				}
				if data.Payload != base64.StdEncoding.EncodeToString([]byte("abc")) || !data.Truncated || data.Original != 6 {
					t.Fatal(data)
				}
			}
		})
	}
}

func TestRejectInvalidConfiguration(t *testing.T) {
	if _, err := Start(t.Context(), profiles.Snapshot{TypeID: "tcp-banner", TypeVersion: 1, ProfileRevision: 1, Config: map[string]any{}}, "127.0.0.1", func(context.Context, events.AgentEvent) error { return nil }); err == nil {
		t.Fatal("accepted invalid config")
	}
}
