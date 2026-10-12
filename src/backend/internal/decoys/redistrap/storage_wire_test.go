package redistrap

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"honey-forge/modules/events"
)

func TestRedisWireRejectsStorageExhaustionAndKeepsServing(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(t.Context(), testSnapshot(port), "127.0.0.1", func(context.Context, events.AgentEvent) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Stop)
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	exchange := func(command string) string {
		t.Helper()
		if _, err := conn.Write([]byte(command)); err != nil {
			t.Fatal(err)
		}
		reply, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	if reply := exchange("SET unauth value\r\n"); !strings.HasPrefix(reply, "-NOAUTH ") {
		t.Fatalf("unauthenticated write accepted: %q", reply)
	}
	if reply := exchange("AUTH bait-pass\r\n"); reply != "+OK\r\n" {
		t.Fatalf("AUTH: %q", reply)
	}
	value := strings.Repeat("x", 4096)
	blocked := false
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key:%d", i)
		command := fmt.Sprintf("*3\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(key), key, len(value), value)
		reply := exchange(command)
		if strings.HasPrefix(reply, "-OOM ") {
			blocked = true
			if reply := exchange("EXISTS " + key + "\r\n"); reply != ":0\r\n" {
				t.Fatalf("rejected SET retained its key: %q", reply)
			}
			break
		}
		if reply != "+OK\r\n" {
			t.Fatalf("SET: %q", reply)
		}
	}
	if !blocked {
		t.Fatal("network client bypassed storage budget")
	}
	if reply := exchange("PING\r\n"); reply != "+PONG\r\n" {
		t.Fatalf("trap stopped after rejected SET: %q", reply)
	}
	if err := runtime.Err(); err != nil {
		t.Fatalf("resource limit failed runtime: %v", err)
	}
}
