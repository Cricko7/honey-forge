//go:build integration

package dionaeatrap

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestRealDionaeaProtocols(t *testing.T) {
	if os.Getenv("TEST_DIONAEA_RUNTIME") != "1" {
		t.Skip("pinned Linux Dionaea runtime required")
	}
	t.Setenv("NM_STDIO", "1")
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	smb := make([]byte, 32)
	copy(smb, []byte{0xff, 'S', 'M', 'B', 0x72})
	smb[9] = 0x18
	binary.LittleEndian.PutUint16(smb[10:], 0x2801)
	smb = append(smb, []byte("\x00\x0c\x00\x02NT LM 0.12\x00")...)
	smb = append([]byte{0, 0, 0, byte(len(smb))}, smb...)
	for _, tc := range []struct{ name, probe, response string }{
		{"smb", hex.EncodeToString(smb), "ff534d42"},
		{"mssql", "12010028000001000000000000000000000000000000000000000000ff0000000000000000000000", "04"},
		{"mysql", "", "0a"}, // Server greeting precedes client authentication.
		{"mqtt", "c000", "d000"},
		{"http", hex.EncodeToString([]byte("GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")), hex.EncodeToString([]byte("HTTP/1."))},
		{"memcache", hex.EncodeToString([]byte("version\r\n")), hex.EncodeToString([]byte("VERSION"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			snapshot := profiles.Snapshot{TypeID: "dionaea", TypeVersion: 1, ProfileRevision: 2, Config: profiles.Object{
				"services":   []any{map[string]any{"name": "service", "port": port, "protocol": tc.name}},
				"session":    map[string]any{"max_connections": 16, "max_seconds": 30},
				"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 500},
			}}
			observed := make(chan events.AgentEvent, 64)
			r, err := Start(t.Context(), snapshot, "127.0.0.1", func(ctx context.Context, e events.AgentEvent) error {
				if err := catalogue.CheckEvent(ctx, "dionaea", 1, e.EventType, e.Data); err != nil {
					return err
				}
				observed <- e
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Stop()
			conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			probe, err := hex.DecodeString(tc.probe)
			if err != nil {
				t.Fatal(err)
			}
			if len(probe) > 0 {
				if _, err := conn.Write(probe); err != nil {
					t.Fatal(err)
				}
			}
			buf := make([]byte, 8192)
			n, err := conn.Read(buf)
			want, decodeErr := hex.DecodeString(tc.response)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if err != nil || !bytes.Contains(buf[:n], want) {
				t.Fatalf("response %x: %v; engine %v", buf[:n], err, r.Err())
			}
			if bytes.Contains(bytes.ToLower(buf[:n]), []byte("honeyforge")) {
				t.Fatal("control identity in protocol response")
			}
			r.Stop()
			seen := map[string]bool{}
			for len(observed) > 0 {
				e := <-observed
				seen[e.EventType] = true
				if e.TypeID != "dionaea" || e.Destination.Port != port || e.Destination.Protocol != "tcp" {
					t.Fatal(e)
				}
			}
			if !seen["service.connection_opened"] || !seen["service.connection_closed"] || (len(probe) > 0 && !seen["service.action"]) {
				t.Fatal("incomplete lifecycle", seen)
			}
		})
	}
}

func TestRealDionaeaLifecycleAndJournalFailure(t *testing.T) {
	if os.Getenv("TEST_DIONAEA_RUNTIME") != "1" {
		t.Skip("pinned Linux Dionaea runtime required")
	}
	t.Setenv("NM_STDIO", "1")
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "close all sessions", true: "journal failure"}[fail], func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			snapshot := profiles.Snapshot{TypeID: "dionaea", TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{
				"services":   []any{map[string]any{"name": "broker", "port": port, "protocol": "mqtt"}},
				"session":    map[string]any{"max_connections": 3, "max_seconds": 30},
				"management": map[string]any{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 500},
			}}
			observed := make(chan events.AgentEvent, 64)
			failure := errors.New("journal unavailable")
			r, err := Start(t.Context(), snapshot, "127.0.0.1", func(_ context.Context, e events.AgentEvent) error {
				if fail && e.EventType == "service.action" {
					return failure
				}
				observed <- e
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Stop()
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			var first net.Conn
			for range 3 {
				conn, err := net.DialTimeout("tcp4", address, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				first = conn
				select {
				case e := <-observed:
					if e.EventType != "service.connection_opened" {
						t.Fatal(e)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("no session opening")
				}
			}
			if fail {
				if _, err := first.Write([]byte{0xc0, 0}); err != nil {
					t.Fatal(err)
				}
				select {
				case <-r.Done():
				case <-time.After(3 * time.Second):
					t.Fatal("engine survived journal failure")
				}
				if !errors.Is(r.Err(), failure) {
					t.Fatal(r.Err())
				}
				if conn, err := net.DialTimeout("tcp4", address, time.Second); err == nil {
					conn.Close()
					t.Fatal("listener survived telemetry failure")
				}
			} else {
				r.Stop()
				closed := 0
				for len(observed) > 0 {
					if (<-observed).EventType == "service.connection_closed" {
						closed++
					}
				}
				if closed != 3 {
					t.Fatalf("persisted %d final events, want 3", closed)
				}
			}
		})
	}
}
