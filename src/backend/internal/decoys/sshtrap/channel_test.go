package sshtrap

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func startTestGateway(t *testing.T, b *fakeBackend, configure func(*profiles.Snapshot), sink Sink) (*Runtime, *ssh.Client) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	snapshot := testSnapshot(port)
	if configure != nil {
		configure(&snapshot)
	}
	if sink == nil {
		sink = func(context.Context, events.AgentEvent) error { return nil }
	}
	r, err := start(t.Context(), snapshot, "127.0.0.1", sink, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	client, err := ssh.Dial("tcp", r.listener.Addr().String(), &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.Password("bait")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return r, client
}

func TestPTYModesAndDimensionsReachSandboxUnchanged(t *testing.T) {
	requests := make(chan []byte, 1)
	b := &fakeBackend{onRequest: func(req *ssh.Request) {
		if req.Type == "pty-req" {
			requests <- bytes.Clone(req.Payload)
		}
	}}
	_, client := startTestGateway(t, b, nil, nil)
	s, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	payload := ssh.Marshal(struct {
		Term                         string
		Columns, Rows, Width, Height uint32
		Modes                        string
	}{"xterm", 120, 40, 960, 640, string(append(ssh.Marshal(struct {
		Opcode byte
		Value  uint32
	}{ssh.ECHO, 0}), 0))})
	ok, err := s.SendRequest("pty-req", true, payload)
	if err != nil || !ok {
		t.Fatalf("PTY rejected: %v", err)
	}
	select {
	case got := <-requests:
		if !bytes.Equal(got, payload) {
			t.Fatalf("terminal settings changed: got %x, want %x", got, payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PTY never reached sandbox")
	}
}

func TestGatewayRejectsForwardingAndStopsActiveShell(t *testing.T) {
	b := &fakeBackend{}
	r, client := startTestGateway(t, b, nil, nil)
	if conn, err := client.Dial("tcp", "127.0.0.1:80"); err == nil {
		conn.Close()
		t.Fatal("TCP forwarding accepted")
	}
	if listener, err := client.Listen("tcp", "127.0.0.1:0"); err == nil {
		listener.Close()
		t.Fatal("remote TCP forwarding accepted")
	}
	s, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown stuck on active shell")
	}
	if b.opened.Load() != 1 || b.closed.Load() != 1 {
		t.Fatalf("cleanup count: %d/%d", b.opened.Load(), b.closed.Load())
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
}
