package redistrap

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"honey-forge/modules/events"
)

func TestResponsesCorrelateEvenWhenReadBeforeRequests(t *testing.T) {
	var got []events.AgentEvent
	e := &emitter{ctx: t.Context(), source: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, snapshot: testSnapshot(6380), sink: func(_ context.Context, event events.AgentEvent) error {
		got = append(got, event)
		return nil
	}}
	o := &sessionObserver{emitter: e, service: "redis"}
	for _, wire := range []string{"-WRONGPASS invalid credentials\r\n", "+OK\r\n"} {
		r, err := readResponse(bufio.NewReader(strings.NewReader(wire)))
		if err != nil {
			t.Fatal(err)
		}

		if err := o.reply(r); err != nil {
			t.Fatal(err)
		}
	}

	for _, password := range []string{"wrong", "bait-pass"} {
		if err := o.request(observedCommand{args: []string{"AUTH", password}, count: 20}); err != nil {
			t.Fatal(err)
		}
	}

	if len(got) != 4 {
		t.Fatalf("missing commands or replies: %+v", got)
	}

	for i, outcome := range []string{"rejected", "accepted"} {
		var data struct{ Outcome, Input string }
		if err := json.Unmarshal(got[2*i].Data, &data); err != nil || data.Outcome != outcome {
			t.Fatalf("authentication result: %+v %v", data, err)
		}

		if err := json.Unmarshal(got[2*i+1].Data, &data); err != nil || data.Outcome != outcome || !strings.HasPrefix(data.Input, "Redis response: ") {
			t.Fatalf("missing original Redis reply: %+v %v", data, err)
		}
	}

	if !strings.Contains(string(got[1].Data), "WRONGPASS") {
		t.Fatal("WRONGPASS was lost")
	}
}

func TestResponseFrames(t *testing.T) {
	for _, wire := range []string{
		"+OK\r\n", "-WRONGPASS invalid credentials\r\n", "$-1\r\n", "_\r\n", "#t\r\n", ",1.2\r\n", "(1234567890\r\n",
		"*2\r\n:1\r\n$3\r\na\x00b\r\n", "%1\r\n+key\r\n~1\r\n:42\r\n", ">2\r\n+message\r\n$1\r\nx\r\n", "!3\r\nERR\r\n", "=7\r\ntxt:abc\r\n",
		"|1\r\n+key\r\n+value\r\n", "$10000\r\n" + strings.Repeat("x", 10000) + "\r\n",
	} {
		t.Run(wire[:min(len(wire), 15)], func(t *testing.T) {
			r, err := readResponse(bufio.NewReader(strings.NewReader(wire)))
			if err != nil || r.count != int64(len(wire)) || r.wire != wire[:min(len(wire), 4096)] {
				t.Fatalf("response=%+v err=%v", r, err)
			}
		})
	}
}

func TestStreamingResponsesAreNotAssignedToLaterCommands(t *testing.T) {
	var got []events.AgentEvent
	e := &emitter{ctx: t.Context(), source: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, snapshot: testSnapshot(6380), sink: func(_ context.Context, event events.AgentEvent) error {
		got = append(got, event)
		return nil
	}}
	o := &sessionObserver{emitter: e, service: "redis"}
	for _, args := range [][]string{{"SUBSCRIBE", "news"}, {"PING"}, {"CLIENT", "REPLY", "OFF"}, {"SET", "x", "y"}} {
		if err := o.request(observedCommand{args: args, count: 20}); err != nil {
			t.Fatal(err)
		}
	}

	if err := o.reply(observedResponse{wire: "*3\r\n+message\r\n+news\r\n+hello\r\n", kind: '*', count: 34}); err != nil {
		t.Fatal(err)
	}

	if len(got) != 5 || len(o.commands) != 0 || len(o.responses) != 0 {
		t.Fatalf("stream messages were assigned to commands: %+v", got)
	}

	for _, event := range got[:4] {
		var data struct{ Outcome string }
		if err := json.Unmarshal(event.Data, &data); err != nil || data.Outcome != "unknown" {
			t.Fatalf("invented streaming result: %s %v", event.Data, err)
		}
	}
}
