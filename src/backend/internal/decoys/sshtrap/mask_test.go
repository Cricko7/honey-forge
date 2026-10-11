package sshtrap

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"honey-forge/modules/events"
)

func TestPublicSSHBanner(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	r, err := start(t.Context(), testSnapshot(port), "127.0.0.1", func(context.Context, events.AgentEvent) error { return nil }, &fakeBackend{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	conn, err := net.Dial("tcp", r.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// SSH identification is exchanged in both directions before key exchange.
	if _, err := conn.Write([]byte("SSH-2.0-OpenSSH_9.2p1\r\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "SSH-2.0-OpenSSH_") || strings.Contains(line, "Go") {
		t.Fatalf("fingerprinting banner: %q", line)
	}
}
