package agent

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

func TestStdioReadDeadlineSurvivesWriteDeadlineReset(t *testing.T) {
	input, peer := net.Pipe()
	defer peer.Close()
	output, other := net.Pipe()
	defer other.Close()
	conn := &stdioConn{reader: input, input: input, output: output}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := conn.Read(make([]byte, 1)); done <- err }()
	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("read timeout: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("clearing write deadline disabled read timeout")
	}
}

func TestStdioBlockedWriteTimesOut(t *testing.T) {
	input, peer := net.Pipe()
	defer peer.Close()
	output, other := net.Pipe()
	defer other.Close()
	conn := &stdioConn{reader: input, input: input, output: output}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := conn.Write([]byte("blocked")); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("write timeout: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked pipe write ignored deadline")
	}
}
