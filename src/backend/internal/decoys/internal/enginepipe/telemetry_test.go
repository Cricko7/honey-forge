package enginepipe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"honey-forge/modules/events"
)

const sessionID = "11111111-1111-4111-8111-111111111111"

func TestTelemetrySessionAndPinnedIdentity(t *testing.T) {
	s := testSnapshot(t)
	c, err := ParseConfig(t.Context(), s, "conpot")
	if err != nil {
		t.Fatal(err)
	}
	b := bridge{snapshot: s, config: c, sessions: map[string]*session{}}
	var got []events.AgentEvent
	sink := func(_ context.Context, e events.AgentEvent) error { got = append(got, e); return nil }
	for _, m := range []message{
		{Kind: "opened", SessionID: sessionID, SourceIP: "192.0.2.1", SourcePort: 41234},
		{Kind: "action", SessionID: sessionID, Input: "READ_COILS request=000100000006010100010001", ReceivedBytes: 12},
		{Kind: "closed", SessionID: sessionID, BytesReceived: 12, DurationMS: 5, Reason: "peer_closed"},
	} {
		if err := b.accept(t.Context(), m, sink); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 3 || len(b.sessions) != 0 {
		t.Fatalf("events=%v sessions=%v", got, b.sessions)
	}
	for i, e := range got {
		if e.SessionSequence != int64(i+1) || e.TypeID != "conpot" || e.ProfileRevision != 2 || e.Source.IP != "192.0.2.1" || e.Destination.Port != 1502 || e.Destination.Protocol != "tcp" {
			t.Fatalf("event: %+v", e)
		}
		var data map[string]any
		if err := json.Unmarshal(e.Data, &data); err != nil || data["service"] != "plc" {
			t.Fatalf("data: %s", e.Data)
		}
	}
}

func TestTelemetryRejectsInvalidOrUnboundedMessages(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    message
	}{
		{"action without open", message{Kind: "action", SessionID: sessionID}},
		{"invalid IP", message{Kind: "opened", SessionID: sessionID, SourceIP: "api.internal", SourcePort: 1}},
		{"invalid identity", message{Kind: "opened", SessionID: "bad", SourceIP: "192.0.2.1", SourcePort: 1}},
		{"invalid port", message{Kind: "opened", SessionID: sessionID, SourceIP: "192.0.2.1", SourcePort: 0}},
		{"unknown kind", message{Kind: "secret", SessionID: sessionID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSnapshot(t)
			c, err := ParseConfig(t.Context(), s, "conpot")
			if err != nil {
				t.Fatal(err)
			}
			b := bridge{snapshot: s, config: c, sessions: map[string]*session{}}
			if err := b.accept(t.Context(), tc.m, func(context.Context, events.AgentEvent) error { t.Fatal("invalid event reached sink"); return nil }); err == nil {
				t.Fatal("accepted invalid telemetry")
			}
		})
	}
}

func TestTelemetryFailsClosedAndBoundsInput(t *testing.T) {
	s := testSnapshot(t)
	c, err := ParseConfig(t.Context(), s, "conpot")
	if err != nil {
		t.Fatal(err)
	}
	b := bridge{snapshot: s, config: c, sessions: map[string]*session{}}
	sink := func(context.Context, events.AgentEvent) error { return nil }
	open := message{Kind: "opened", SessionID: sessionID, SourceIP: "192.0.2.1", SourcePort: 40000}
	if err := b.accept(t.Context(), open, sink); err != nil {
		t.Fatal(err)
	}
	if err := b.accept(t.Context(), open, sink); err == nil {
		t.Fatal("accepted duplicate open")
	}
	if err := b.accept(t.Context(), message{Kind: "action", SessionID: sessionID, Input: strings.Repeat("x", 1025)}, sink); err == nil {
		t.Fatal("accepted oversized input")
	}
	failure := errors.New("journal unavailable")
	if err := b.accept(t.Context(), message{Kind: "action", SessionID: sessionID, Input: "READ"}, func(context.Context, events.AgentEvent) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("lost sink error: %v", err)
	}
	if b.sessions[sessionID].sequence != 1 {
		t.Fatal("failed delivery advanced sequence")
	}
}
