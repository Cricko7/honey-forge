package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/modules/profiles"
)

func TestLocalPipelineSeparatesNotificationsFromAcknowledgements(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	local, err := NewLocal(j, "token")
	if err != nil {
		t.Fatal(err)
	}
	const target = "33333333-3333-4333-8333-333333333333"
	local.Configure(profiles.Snapshot{TypeID: "custom-http", Config: profiles.Object{
		"services": []any{map[string]any{"name": "web", "entry": "web"}},
		"blocks":   []any{map[string]any{"id": "web", "kind": "input", "config": map[string]any{"method": "POST", "path": "/pipeline"}}, map[string]any{"id": "send", "kind": "send", "config": map[string]any{"target_id": target, "service": "web", "method": "POST", "path": "/pipeline"}}},
	}})
	tls, certificate, err := newLocalTLS()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(local.Handler(t.Context()))
	server.TLS = tls
	server.StartTLS()
	defer server.Close()
	client, err := DialLocal(t.Context(), "wss"+strings.TrimPrefix(server.URL, "https")+"/trap-stream", "token", certificate)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	r := pipeline.Request{RequestID: string(contract.NewID()), NodeID: "send", TargetID: target, Service: "web", Method: "POST", Path: "/pipeline", Payload: json.RawMessage(`{"count":3}`), SourceIP: "127.0.0.1", SourcePort: 1234}
	d := pipeline.Delivery{Request: r, SourceID: target, TraceID: string(contract.NewID()), Hop: 1, ExpiresAt: time.Now().Add(pipeline.Lifetime)}
	d.Token = string(contract.NewID())
	done := make(chan error, 1)
	go func() { done <- client.SendPipeline(t.Context(), r) }()
	select {
	case item := <-local.PipelineOutgoing:
		local.PipelineIncoming <- d
		item.result <- true
	case <-time.After(3 * time.Second):
		t.Fatal("missing outgoing handoff")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing acknowledgement")
	}
	select {
	case got := <-client.PipelineIncoming:
		if string(got.Payload) != string(r.Payload) {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing incoming notification")
	}
	r.TargetID = string(contract.NewID())
	if err := client.SendPipeline(context.Background(), r); err == nil {
		t.Fatal("unconfigured destination accepted")
	}
	if err := client.Ready(t.Context()); err != nil {
		t.Fatal("rejection broke telemetry connection", err)
	}
}
