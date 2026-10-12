package sshtrap

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestPipeHelperProcess(t *testing.T) {
	if os.Getenv("HF_PIPE_HELPER") != "1" {
		return
	}

	if os.Getenv("HF_PIPE_HELPER_WAIT") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
	} else {
		_, _ = os.Stdout.Write([]byte("last SSH packet\n"))
	}

	os.Exit(0)
}

func helperPipe(t *testing.T, wait bool) *execPipe {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPipeHelperProcess$")
	cmd.Env = append(os.Environ(), "HF_PIPE_HELPER=1")
	if wait {
		cmd.Env = append(cmd.Env, "HF_PIPE_HELPER_WAIT=1")
	}

	p, err := startCommandPipe(cmd)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		p.Close()
	})

	return p
}

func TestPipePreservesBufferedOutputAfterProcessExit(t *testing.T) {
	p := helperPipe(t, false)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not exit")
	}

	output, err := io.ReadAll(p)
	if err != nil || !bytes.Equal(output, []byte("last SSH packet\n")) {
		t.Fatalf("buffered output lost after exit: %q, %v", output, err)
	}
}

func TestPipeDeadlineReleasesBlockedReadAndProcess(t *testing.T) {
	p := helperPipe(t, true)
	if err := p.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = p.Read(make([]byte, 1))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("read remained blocked after deadline")
	}

	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("helper remained running after deadline")
	}

	if err := p.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}
