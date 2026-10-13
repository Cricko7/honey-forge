package sshtrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin/binding"

	"honey-forge/internal/contract"
	"honey-forge/internal/hosttelemetry"
	"honey-forge/modules/events"
)

func TestHostTelemetryMapping(t *testing.T) {
	raw, err := os.ReadFile("../../hosttelemetry/testdata/process_exec.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := hosttelemetry.ParseTetragon(raw, time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	started := parsed.At.Add(-time.Second)
	for _, tc := range []struct {
		name                string
		second, old, failed bool
		count               int
	}{
		{name: "unique truncated ID", count: 1},
		{name: "ambiguous prefix", second: true},
		{name: "old incarnation", old: true},
		{name: "failed journal", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured []events.AgentEvent
			snapshot := testSnapshot(2222)
			snapshot.TypeVersion = 2
			record := newRecording(t.Context(), snapshot, "ssh", "192.0.2.1:50000", 2222, 8192, func(_ context.Context, e events.AgentEvent) error {
				if tc.failed {
					return errors.New("journal unavailable")
				}
				captured = append(captured, e)
				return nil
			})
			o := newHostObserver("host-a", "11111111-1111-4111-8111-111111111111", 3)
			at := started
			if tc.old {
				at = parsed.At.Add(time.Second)
			}
			if err := o.bind(strings.Repeat("a", 64), at, record); err != nil {
				t.Fatal(err)
			}
			if tc.second {
				if err := o.bind(strings.Repeat("a", 63)+"b", started, record); err != nil {
					t.Fatal(err)
				}
			}
			err := o.accept(parsed)
			if (err != nil) != tc.failed || len(captured) != tc.count {
				t.Fatalf("events=%d error=%v", len(captured), err)
			}
			if tc.count == 0 {
				return
			}
			event := captured[0]
			if event.SessionID != record.sessionID || event.OccurredAt != parsed.At.Format(time.RFC3339Nano) || event.Source.IP != "192.0.2.1" {
				t.Fatalf("wrong session or source: %+v", event)
			}
			if strings.Contains(string(event.Data), "super-secret") || !strings.Contains(string(event.Data), `"host_id":"host-a"`) || !strings.Contains(string(event.Data), `"generation":3`) {
				t.Fatalf("wrong metadata: %s", event.Data)
			}
			if err := o.accept(parsed); err != nil || len(captured) != 1 {
				t.Fatalf("duplicate not suppressed: %v", err)
			}
			o.unbind(record.sessionID)
			if err := o.accept(parsed); err != nil || len(captured) != 1 {
				t.Fatalf("destroyed sandbox retained: %v", err)
			}
		})
	}
}

func TestHostTelemetryConcurrencyAndTwoSessions(t *testing.T) {
	o := newHostObserver("host-a", "11111111-1111-4111-8111-111111111111", 1)
	var captured []events.AgentEvent
	snapshot := testSnapshot(2222)
	snapshot.TypeVersion = 2
	var records []*recording
	for _, id := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		r := newRecording(t.Context(), snapshot, "ssh", "192.0.2.1:50000", 2222, 8192, func(_ context.Context, e events.AgentEvent) error {
			captured = append(captured, e)
			return nil
		})
		records = append(records, r)
		if err := o.bind(id, time.Now().Add(-time.Minute), r); err != nil {
			t.Fatal(err)
		}
	}
	var workers sync.WaitGroup
	for _, id := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)} {
		workers.Go(func() {
			if err := o.accept(&hosttelemetry.Exec{ExecID: "one", ContainerID: id, At: time.Now(), PID: 1, Binary: "/usr/bin/id"}); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if len(captured) != 2 || captured[0].SessionID == captured[1].SessionID || captured[0].EventID == captured[1].EventID {
		t.Fatalf("wrong attribution: %+v", captured)
	}
	for _, r := range records {
		o.unbind(r.sessionID)
	}
}

func TestHostTelemetryStreamFailures(t *testing.T) {
	o := newHostObserver("host-a", "11111111-1111-4111-8111-111111111111", 1)
	for _, input := range []string{"", "{broken}\n", strings.Repeat("x", hosttelemetry.MaxRecordBytes+1)} {
		if err := o.consume(strings.NewReader(input)); err == nil {
			t.Fatal("stream failure silently accepted")
		}
	}
	if err := o.bind("unsafe", time.Now(), nil); err == nil {
		t.Fatal("invalid mapping accepted")
	}
}

func TestVersionTwoRequiresSensor(t *testing.T) {
	t.Setenv("NM_TETRAGON_CONTAINER", "")
	snapshot := testSnapshot(2222)
	snapshot.TypeVersion = 2
	if _, err := Start(t.Context(), snapshot, "127.0.0.1", func(context.Context, events.AgentEvent) error { return nil }); err == nil || !strings.Contains(err.Error(), "NM_TETRAGON_CONTAINER") {
		t.Fatalf("unavailable sensor not reported: %v", err)
	}
}

func TestProcessEventSnapshot(t *testing.T) {
	// The event is still a normal validated AgentEvent, not a parallel ingest.
	o := newHostObserver("host-a", "11111111-1111-4111-8111-111111111111", 1)
	snapshot := testSnapshot(2222)
	snapshot.TypeVersion = 2
	r := newRecording(t.Context(), snapshot, "ssh", "192.0.2.1:50000", 2222, 8192, func(_ context.Context, e events.AgentEvent) error {
		contract.Configure()
		if err := binding.Validator.ValidateStruct(e); err != nil {
			t.Fatalf("invalid normalized envelope: %v", err)
		}
		var data map[string]any
		if err := json.Unmarshal(e.Data, &data); err != nil || e.TypeVersion != 2 || data["arguments_redacted"] != true {
			t.Fatalf("bad envelope: %s %v", e.Data, err)
		}
		return nil
	})
	if err := o.bind(strings.Repeat("a", 64), time.Now().Add(-time.Minute), r); err != nil {
		t.Fatal(err)
	}
	if err := o.accept(&hosttelemetry.Exec{ExecID: "one", ContainerID: strings.Repeat("a", 64), At: time.Now(), PID: 1, Binary: "/usr/bin/id"}); err != nil {
		t.Fatal(err)
	}
}
