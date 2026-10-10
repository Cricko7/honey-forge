package sshtrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestRecordingOrdersConcurrentStreamsAndEnforcesBudget(t *testing.T) {
	var got []events.AgentEvent
	r := newRecording(t.Context(), profiles.Snapshot{TypeID: "ssh-high-interaction", TypeVersion: 1, ProfileRevision: 1}, "ssh", "127.0.0.1:12345", 2222, 8192,
		func(_ context.Context, e events.AgentEvent) error { got = append(got, e); return nil })
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := r.io("channel", "stdout", []byte{0, 255}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for i, e := range got {
		if e.SessionSequence != int64(i+1) || e.SessionID != got[0].SessionID {
			t.Fatal("recording lost ordering")
		}
		var d struct {
			Payload string `json:"payload_base64"`
		}
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		b, err := base64.StdEncoding.DecodeString(d.Payload)
		if err != nil || len(b) != 2 || b[1] != 255 {
			t.Fatal("binary recording corrupted")
		}
	}
	if err := r.io("channel", "stdin", make([]byte, 8193)); !errors.Is(err, ErrRecordingLimit) {
		t.Fatalf("limit: %v", err)
	}
	if len(got) != 8 {
		t.Fatal("over-limit bytes recorded")
	}
}

func TestRecordingDoesNotAdvanceOnSinkFailure(t *testing.T) {
	want := errors.New("disk full")
	r := newRecording(t.Context(), profiles.Snapshot{}, "ssh", "127.0.0.1:12345", 2222, 4096, func(context.Context, events.AgentEvent) error { return want })
	if err := r.io("channel", "stdin", []byte("hello")); !errors.Is(err, want) {
		t.Fatalf("failure: %v", err)
	}
	if r.sequence != 0 || r.bytes != 0 {
		t.Fatal("uncommitted bytes counted")
	}
}
