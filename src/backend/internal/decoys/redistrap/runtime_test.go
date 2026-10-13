package redistrap

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func testSnapshot(port int) profiles.Snapshot {
	return profiles.Snapshot{
		TypeID:          "redis-emulator",
		TypeVersion:     1,
		ProfileRevision: 1,
		Config: map[string]any{
			"services":   []any{map[string]any{"name": "redis", "port": port, "password": "bait-pass"}},
			"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
		},
	}
}

func TestInteractiveRedisSession(t *testing.T) {
	if _, err := exec.LookPath("redis-server"); err != nil {
		t.Skip("redis-server required; run in the Redis decoy image")
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	collected := make(chan events.AgentEvent, 64)
	runtime, err := Start(t.Context(), testSnapshot(port), "127.0.0.1", func(_ context.Context, event events.AgentEvent) error {
		collected <- event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	defer runtime.Stop()

	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}

	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	cases := []struct{ command, response string }{
		{"*2\r\n$3\r\nGET\r\n$4\r\ntest\r\n", "-NOAUTH Authentication required.\r\n"},
		{"AUTH wrong\r\n", "-WRONGPASS invalid username-password pair or user is disabled.\r\n"},
		{"AUTH\r\n", "-ERR wrong number of arguments for 'auth' command\r\n"},
		{"AUTH bait-pass\r\n", "+OK\r\n"},
		{"SET demo value\r\n", "+OK\r\n"},
		{"GET demo\r\n", "$5\r\nvalue\r\n"},
		{"DEL demo\r\n", ":1\r\n"},
		{"GET demo\r\n", "$-1\r\n"},
		{"QUIT\r\n", "+OK\r\n"},
	}
	for i, tc := range cases {
		if i < 4 {
			if _, err := conn.Write([]byte(tc.command)); err != nil {
				t.Fatal(err)
			}
		} else if i == 4 {
			// Pipeline authenticated commands; failed AUTH pipelines can suppress replies in Redis.
			for _, queued := range cases[4:] {
				if _, err := conn.Write([]byte(queued.command)); err != nil {
					t.Fatal(err)
				}
			}
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}

		if strings.HasPrefix(line, "$5") {
			rest, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}

			line += rest
		}

		if line != tc.response {
			t.Fatalf("%q returned %q, want %q", tc.command, line, tc.response)
		}
	}

	var got []events.AgentEvent
	for {
		select {
		case event := <-collected:
			got = append(got, event)
			if event.EventType == "service.connection_closed" {
				goto done
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing close event")
		}
	}

done:
	if len(got) != 20 {
		t.Fatalf("events = %d, want 20: %+v", len(got), got)
	}
	for i, event := range got {
		if event.SessionSequence != int64(i+1) || event.SessionID != got[0].SessionID || event.TypeID != "redis-emulator" || event.Destination.Port != port {
			t.Fatalf("event %d: %+v", i, event)
		}
	}

	if got[0].EventType != "service.connection_opened" || got[3].EventType != "service.auth_attempt" || got[5].EventType != "service.auth_attempt" {
		t.Fatal("missing lifecycle or auth events")
	}

	if got[0].Source.IP != "127.0.0.1" || got[0].Source.Port == 0 || got[0].OccurredAt == "" {
		t.Fatalf("missing source or timestamp: %+v", got[0])
	}

	var action struct {
		Input         string `json:"input"`
		Outcome       string `json:"outcome"`
		ReceivedBytes int    `json:"received_bytes"`
	}
	if err := json.Unmarshal(got[9].Data, &action); err != nil || action.Input != "SET demo value" || action.Outcome != "accepted" || action.ReceivedBytes != len("SET demo value\r\n") {
		t.Fatalf("command telemetry: %+v %v", action, err)
	}

	var closed struct {
		BytesReceived int    `json:"bytes_received"`
		Reason        string `json:"reason"`
	}
	if err := json.Unmarshal(got[len(got)-1].Data, &closed); err != nil || closed.Reason != "quit" || closed.BytesReceived == 0 {
		t.Fatalf("close telemetry: %+v %v", closed, err)
	}

	var auth struct {
		Password string `json:"password"`
		Outcome  string `json:"outcome"`
	}
	if err := json.Unmarshal(got[3].Data, &auth); err != nil || auth.Password != "wrong" || auth.Outcome != "rejected" {
		t.Fatalf("rejected auth: %+v %v", auth, err)
	}

	if err := json.Unmarshal(got[7].Data, &auth); err != nil || auth.Password != "bait-pass" || auth.Outcome != "accepted" {
		t.Fatalf("accepted auth: %+v %v", auth, err)
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*profiles.Snapshot)
	}{
		{"wrong type", func(s *profiles.Snapshot) {
			s.TypeID = "tcp-banner"
		}},
		{"missing password", func(s *profiles.Snapshot) {
			delete(s.Config["services"].([]any)[0].(map[string]any), "password")
		}},
		{"invalid port", func(s *profiles.Snapshot) {
			s.Config["services"].([]any)[0].(map[string]any)["port"] = 0
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSnapshot(2222)
			tc.change(&s)
			if _, err := Start(t.Context(), s, "127.0.0.1", func(context.Context, events.AgentEvent) error {
				return nil
			}); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
