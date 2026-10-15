//go:build integration

package conpottrap

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
)

// Native protocol checks complement the Docker isolation test. Bind loopback
// only; TEST_CONPOT_RUNTIME requires the pinned engine and dependencies.
func TestRealConpotProtocols(t *testing.T) {
	if os.Getenv("TEST_CONPOT_RUNTIME") != "1" {
		t.Skip("TEST_CONPOT_RUNTIME=1 and HF_PYTHON with pinned Conpot required")
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
	for _, tc := range []struct {
		name, transport, probe string
		first                  byte
	}{
		{"modbus", "tcp", "000100000006010100010001", 0x00},
		{"s7comm", "tcp", "0300001611e00000000600c1020100c2020102c0010a", 0x03},
		{"enip", "tcp", "630000000000000000000000000000000000000000000000", 0x63},
		{"snmp", "udp", "302602010104067075626c6963a019020101020100020100300e300c06082b060102010101000500", 0x30},
		{"bacnet", "udp", "810a001101040005010c0c023fffff1978", 0x81},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var port int
			if tc.transport == "tcp" {
				l, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				port = l.Addr().(*net.TCPAddr).Port
				if err := l.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				l, err := net.ListenPacket("udp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				port = l.LocalAddr().(*net.UDPAddr).Port
				if err := l.Close(); err != nil {
					t.Fatal(err)
				}
			}
			s := testSnapshot(t)
			s.Config["services"] = []any{map[string]any{"name": "plc", "port": port, "protocol": tc.name}}
			observed := make(chan events.AgentEvent, 64)
			r, err := Start(t.Context(), s, "127.0.0.1", func(ctx context.Context, e events.AgentEvent) error {
				if err := catalogue.CheckEvent(ctx, "conpot", 1, e.EventType, e.Data); err != nil {
					return err
				}
				observed <- e
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Stop()
			conn, err := net.DialTimeout(tc.transport, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(4 * time.Second)); err != nil {
				t.Fatal(err)
			}
			probe, err := hex.DecodeString(tc.probe)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write(probe); err != nil {
				t.Fatal(err)
			}
			response := make([]byte, 8192)
			n, err := conn.Read(response)
			if err != nil || n == 0 || response[0] != tc.first {
				t.Fatalf("response %x error %v runtime %v", response[:n], err, r.Err())
			}
			if strings.Contains(strings.ToLower(string(response[:n])), "honeyforge") {
				t.Fatal("control identity in decoy response")
			}
			timeout := time.NewTimer(4 * time.Second)
			defer timeout.Stop()
			for {
				select {
				case e := <-observed:
					if e.EventType == "service.action" {
						if e.Destination.Protocol != tc.transport || e.Destination.Port != port {
							t.Fatal(e)
						}
						return
					}
				case <-timeout.C:
					t.Fatalf("no action event; runtime: %v", r.Err())
				}
			}
		})
	}
}

func TestRealConpotClosesAllSessionsBeforeExit(t *testing.T) {
	if os.Getenv("TEST_CONPOT_RUNTIME") != "1" {
		t.Skip("TEST_CONPOT_RUNTIME=1 required")
	}
	t.Setenv("NM_STDIO", "1")
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	s := testSnapshot(t)
	s.Config["services"] = []any{map[string]any{"name": "plc", "port": port, "protocol": "modbus"}}
	observed := make(chan events.AgentEvent, 64)
	r, err := Start(t.Context(), s, "127.0.0.1", func(_ context.Context, e events.AgentEvent) error { observed <- e; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	for range 3 {
		conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		select {
		case e := <-observed:
			if e.EventType != "service.connection_opened" {
				t.Fatal(e)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no connection opening")
		}
	}
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

func TestRealConpotStopsOnJournalFailure(t *testing.T) {
	if os.Getenv("TEST_CONPOT_RUNTIME") != "1" {
		t.Skip("TEST_CONPOT_RUNTIME=1 required")
	}
	t.Setenv("NM_STDIO", "1")
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	s := testSnapshot(t)
	s.Config["services"] = []any{map[string]any{"name": "plc", "port": port, "protocol": "modbus"}}
	failure := errors.New("journal unavailable")
	r, err := Start(t.Context(), s, "127.0.0.1", func(_ context.Context, e events.AgentEvent) error {
		if e.EventType == "service.action" {
			return failure
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp4", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request, err := hex.DecodeString("000100000006010100010001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("runtime continued after journal failure")
	}
	if !errors.Is(r.Err(), failure) {
		t.Fatalf("missing journal failure: %v", r.Err())
	}
	if next, err := net.DialTimeout("tcp4", address, time.Second); err == nil {
		next.Close()
		t.Fatal("listener survived telemetry failure")
	}
}
