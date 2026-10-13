package redistrap

import (
	"bufio"
	"context"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"honey-forge/modules/events"
)

func TestRealRedisSharedData(t *testing.T) {
	if _, err := exec.LookPath("redis-server"); err != nil {
		t.Skip("redis-server required; run in the Redis decoy image")
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := Start(t.Context(), testSnapshot(port), "127.0.0.1", func(context.Context, events.AgentEvent) error { return nil })
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(r.Stop)

	connect := func() net.Conn {
		t.Helper()

		c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { c.Close() })
		if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}

		return c
	}

	exchange := func(c net.Conn, command, expected string) {
		t.Helper()

		if _, err := io.WriteString(c, command); err != nil {
			t.Fatal(err)
		}

		b := make([]byte, len(expected))
		if _, err := io.ReadFull(c, b); err != nil {
			t.Fatal(err)
		}

		if string(b) != expected {
			t.Fatalf("%q: got %q, want %q", command, b, expected)
		}
	}

	a, b := connect(), connect()
	exchange(a, "HSET cart:demo product:1001 2\r\n", "-NOAUTH Authentication required.\r\n")
	exchange(a, "AUTH wrong\r\n", "-WRONGPASS invalid username-password pair or user is disabled.\r\n")
	exchange(a, "AUTH bait-pass\r\n", "+OK\r\n")
	exchange(b, "AUTH bait-pass\r\n", "+OK\r\n")
	exchange(a, "HSET cart:demo product:1001 2\r\n", ":1\r\n")
	exchange(b, "HGET cart:demo product:1001\r\n", "$1\r\n2\r\n")
	exchange(b, "EXISTS user:1001 product:1001 orders:recent\r\n", ":3\r\n")
	exchange(a, "EVAL \"return 40+2\" 0\r\n", ":42\r\n")

	if _, err := io.WriteString(a, "INFO server\r\n"); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(a)
	header, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	size, err := strconv.Atoi(strings.TrimSpace(header[1:]))
	if err != nil {
		t.Fatal(err)
	}

	body := make([]byte, size+2)
	if _, err := io.ReadFull(reader, body); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), "redis_version:") || !strings.Contains(string(body), "process_id:") {
		t.Fatalf("not real Redis: %s", body)
	}

	malformed := connect()
	if _, err := io.WriteString(malformed, "*x\r\n"); err != nil {
		t.Fatal(err)
	}

	line, err := bufio.NewReader(malformed).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "-ERR Protocol error:") {
		t.Fatalf("Redis protocol error was lost: %q %v", line, err)
	}
}
