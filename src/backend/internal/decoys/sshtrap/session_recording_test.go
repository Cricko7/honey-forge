package sshtrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestRecordingFailureClosesActiveConnectionAndReportsError(t *testing.T) {
	want := errors.New("journal unavailable")
	b := &fakeBackend{}
	r, client := startTestGateway(t, b, nil, func(_ context.Context, e events.AgentEvent) error {
		if e.EventType == "service.session_io" {
			return want
		}

		return nil
	})
	s, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	if _, err := s.Output("id"); err == nil {
		t.Fatal("unrecorded command accepted")
	}

	r.Stop()
	if !errors.Is(r.Err(), want) {
		t.Fatalf("missing recording failure: %v", r.Err())
	}

	if b.closed.Load() != 1 {
		t.Fatal("sandbox not removed after journal failure")
	}
}

func TestRecordingLimitClosesOnlyAffectedConnection(t *testing.T) {
	var mu sync.Mutex
	var captured []events.AgentEvent
	b := &fakeBackend{}
	r, client := startTestGateway(t, b, func(s *profiles.Snapshot) {
		s.Config["session"].(map[string]any)["max_connections"] = 2
	}, func(_ context.Context, e events.AgentEvent) error {
		mu.Lock()
		defer mu.Unlock()

		captured = append(captured, e)

		return nil
	})
	healthy, err := ssh.Dial("tcp", r.listener.Addr().String(), &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.Password("bait")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	defer healthy.Close()

	s, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	stdin, err := s.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := stdin.Write(bytes.Repeat([]byte("x"), 8193))
		if err == nil {
			err = s.Wait()
		}

		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("over-limit shell succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recording limit did not disconnect shell")
	}

	other, err := healthy.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	defer other.Close()

	if output, err := other.Output("id"); err != nil || string(output) != "real-backend-output\n" {
		t.Fatalf("other connection affected: %q %v", output, err)
	}

	healthy.Close()
	r.Stop()
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	var limited bool
	for _, e := range captured {
		if e.EventType != "service.connection_closed" {
			continue
		}

		var d struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}

		limited = limited || d.Reason == "recording_limit"
	}

	if !limited {
		t.Fatal("limit reason not recorded")
	}
}

func TestShellRecordsInputAndOutput(t *testing.T) {
	var mu sync.Mutex
	var captured []events.AgentEvent
	r, client := startTestGateway(t, &fakeBackend{}, nil, func(_ context.Context, e events.AgentEvent) error {
		mu.Lock()
		defer mu.Unlock()

		captured = append(captured, e)

		return nil
	})
	s, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	stdin, err := s.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	stdout, err := s.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}

	want := []byte("echo demo\n")
	if _, err := stdin.Write(want); err != nil {
		t.Fatal(err)
	}

	got := make([]byte, len(want))
	if _, err := io.ReadFull(stdout, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("shell output: %q %v", got, err)
	}

	client.Close()
	r.Stop()
	mu.Lock()
	defer mu.Unlock()

	streams := map[string]bool{}
	for _, e := range captured {
		if e.EventType != "service.session_io" {
			continue
		}

		var d struct {
			Stream string `json:"stream"`
		}
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}

		streams[d.Stream] = true
	}

	if !streams["stdin"] || !streams["stdout"] {
		t.Fatal("shell recording misses input or output")
	}
}
