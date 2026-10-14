package pythonhttp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestSeparateHTTPRuntimesChainBeyondNestedCallLimit(t *testing.T) {
	ids := []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}
	runtimes := map[string]*Runtime{}
	final := make(chan events.AgentEvent, 2)
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	var firstPort int
	for index, id := range ids {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			firstPort = port
		}
		snapshot := pipelineSnapshot(port, ids[1-index])
		ctx := pipeline.WithSender(t.Context(), func(ctx context.Context, r pipeline.Request) error {
			if !pipeline.AllowsSend(snapshot, r) {
				t.Error("unconfigured send")
			}
			r.Token = string(contract.NewID())
			return runtimes[r.TargetID].Receive(ctx, pipeline.Delivery{Request: r, SourceID: id, TraceID: ids[0], Hop: 1, ExpiresAt: time.Now().Add(pipeline.Lifetime)})
		})
		runtime, err := Start(ctx, snapshot, "127.0.0.1", func(ctx context.Context, e events.AgentEvent) error {
			if err := cat.CheckEvent(ctx, e.TypeID, 1, e.EventType, e.Data); err != nil {
				t.Logf("invalid %s: %s (%v)", e.EventType, e.Data, err)
				return err
			}
			if e.EventType == "http.failure" {
				t.Logf("flow failure: %s", e.Data)
			}
			if e.EventType == "http.custom" {
				final <- e
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		runtimes[id] = runtime
		t.Cleanup(runtime.Stop)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Post("http://"+net.JoinHostPort("127.0.0.1", strconv.Itoa(firstPort))+"/pipeline", "application/json", strings.NewReader(`{"count":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 202 {
		t.Fatalf("status %d", response.StatusCode)
	}
	select {
	case e := <-final:
		var data struct {
			Payload struct {
				Count int `json:"count"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(e.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.Payload.Count != 40 {
			t.Fatalf("final data: %s", e.Data)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("pipeline did not reach final event")
	}
}

func pipelineSnapshot(port int, target string) profiles.Snapshot {
	block := func(id, kind string, c map[string]any, out map[string]string) any {
		return map[string]any{"id": id, "kind": kind, "config": c, "outputs": out}
	}
	return profiles.Snapshot{TypeID: "custom-http", TypeVersion: 1, ProfileRevision: 1, Config: profiles.Object{
		"services": []any{map[string]any{"name": "web", "port": port, "entry": "web"}},
		"blocks": []any{
			block("web", "input", map[string]any{"name": "web", "port": port, "method": "POST", "path": "/pipeline"}, map[string]string{"out": "parse"}),
			block("parse", "parse", map[string]any{"format": "json"}, map[string]string{"out": "check"}),
			block("check", "condition", map[string]any{"source": "json.count", "operator": "lt", "value": "40"}, map[string]string{"yes": "copy", "no": "notify"}),
			block("copy", "transform", map[string]any{"key": "count", "operation": "assign", "value": "\"${json.count}\""}, map[string]string{"out": "increment"}),
			block("increment", "transform", map[string]any{"key": "count", "operation": "add", "value": "1"}, map[string]string{"out": "send"}),
			block("send", "send", map[string]any{"target_id": target, "service": "web", "method": "POST", "path": "/pipeline", "payload": "{\"count\":\"${vars.count}\"}"}, map[string]string{"out": "reply"}),
			block("notify", "event", map[string]any{"name": "pipeline.done", "severity": "high", "payload": "\"${json}\""}, map[string]string{"out": "reply"}),
			block("reply", "response", map[string]any{"format": "json", "status": 202, "body": "{}", "headers": "{}"}, map[string]string{}),
		}, "limits": map[string]any{"max_steps": 512, "timeout_ms": 10000, "max_body_bytes": 65536},
		"management": map[string]any{"heartbeat_interval_seconds": 10, "telemetry_flush_interval_ms": 500},
	}}
}
