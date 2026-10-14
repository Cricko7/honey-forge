//go:build integration

package app

import (
	"encoding/json"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/modules/agentws"
	"honey-forge/modules/auth"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func TestDurablePipelineRetryIdempotencyAndRevocation(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "durable-pipeline@example.test", auth.OrganizationInput{Mode: "create", Name: "Durable pipeline"})
	config := profiles.Object{
		"services": []any{map[string]any{"name": "web", "port": 32089, "entry": "web"}},
		"blocks": []any{
			map[string]any{"id": "web", "kind": "input", "config": map[string]any{"name": "web", "port": 32089, "method": "POST", "path": "/pipeline"}, "outputs": map[string]string{"out": "reply"}},
			map[string]any{"id": "reply", "kind": "response", "config": map[string]any{"status": 200, "format": "json", "body": "{}", "headers": "{}"}, "outputs": map[string]string{}},
		},
		"limits":     map[string]any{"max_steps": 512, "timeout_ms": 10000, "max_body_bytes": 65536},
		"management": map[string]any{"heartbeat_interval_seconds": 10, "telemetry_flush_interval_ms": 500},
	}

	profile := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, profiles.CreateRequest{RequestID: string(contract.NewID()), Name: "Queue", TypeID: "custom-http", TypeVersion: 1, Config: config}), a, 201))
	create := func(name string) traps.Trap {
		return decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: name, ProfileID: profile.ID}), a, 201))
	}

	source, target := create("Source"), create("Target")
	ctx, sourceIdentity, _, _ := connectIntegrationAgent(t, r, a, source)
	targetCtx, targetIdentity, connection, runtime := connectIntegrationAgent(t, r, a, target)
	var revision int32 = 1
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, targetCtx, targetIdentity, "apply_config", runtime)
	runtime.RuntimeState = "running"
	executeIntegrationCommand(t, r, a, targetCtx, targetIdentity, "start", runtime)
	sendOperator(t, r, "POST", "/api/trap-links", integrationJSON(t, map[string]string{"source_id": source.ID, "target_id": target.ID}), a, 201)
	store := traps.NewRepository(r.pool)
	request := pipeline.Request{RequestID: string(contract.NewID()), NodeID: "send", TargetID: target.ID, Service: "web", Method: "POST", Path: "/pipeline", Payload: json.RawMessage(`{"count":3}`), SourceIP: "127.0.0.1", SourcePort: 1234}
	if err := store.EnqueuePipeline(ctx, sourceIdentity, request); err != nil {
		t.Fatal(err)
	}

	if err := store.EnqueuePipeline(ctx, sourceIdentity, request); err != nil {
		t.Fatalf("identical admission must be idempotent: %v", err)
	}
	request.Payload = json.RawMessage(`{"count":4}`)
	if err := store.EnqueuePipeline(ctx, sourceIdentity, request); err == nil {
		t.Fatal("request ID reused with different data")
	}

	first, err := store.ClaimPipeline(targetCtx, targetIdentity, connection)
	if err != nil || first == nil {
		t.Fatalf("claim: %+v %v", first, err)
	}

	if next, err := store.ClaimPipeline(targetCtx, targetIdentity, connection); err != nil || next != nil {
		t.Fatalf("leased delivery must not be issued again immediately: %+v %v", next, err)
	}

	if _, err := r.pool.Exec(t.Context(), `UPDATE http_pipeline_deliveries SET lease_until=$1`, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	// Recreate the repository to demonstrate recovery without any process-local queue.
	second, err := traps.NewRepository(r.pool).ClaimPipeline(targetCtx, targetIdentity, connection)
	if err != nil || second == nil || second.Token != first.Token || second.RequestID != first.RequestID {
		t.Fatalf("durable retry changed identity: %+v %v", second, err)
	}

	// A revoked organization identity cannot claim another receiver's work.
	foreign := targetIdentity
	foreign.OrganizationID = string(contract.NewID())
	if _, err := store.ClaimPipeline(targetCtx, foreign, connection); err == nil {
		t.Fatal("foreign organization claimed delivery")
	}

	request.Payload = json.RawMessage(`{"count":3}`)
	request.RequestID = string(contract.NewID())
	if err := store.EnqueuePipeline(ctx, sourceIdentity, request); err != nil {
		t.Fatal(err)
	}

	completed, err := store.ClaimPipeline(targetCtx, targetIdentity, connection)
	if err != nil || completed == nil {
		t.Fatalf("claim completion fixture: %+v %v", completed, err)
	}

	data := integrationJSON(t, map[string]any{"service": "web", "pipeline_id": completed.TraceID, "pipeline_step": completed.Hop, "status": 200, "bytes_sent": 0, "payload_base64": "", "truncated": false})
	event := events.AgentEvent{EventID: string(contract.NewID()), EventType: "http.response", TypeID: "custom-http", TypeVersion: 1, ProfileRevision: 1, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), SessionID: string(contract.NewID()), SessionSequence: 1, Source: events.Source{IP: "127.0.0.1", Port: 1234}, Destination: events.Destination{Protocol: "tcp", Port: 32089}, Data: json.RawMessage(data)}
	encoded := json.RawMessage(integrationJSON(t, event))
	batch := agentws.TelemetryBatch{BatchID: string(contract.NewID()), Events: []agentws.Event{{EventID: event.EventID, Raw: encoded}}}
	if _, err := r.Agents.Ingest(targetCtx, targetIdentity, connection, batch); err != nil {
		t.Fatalf("persist completion receipt: %v", err)
	}

	var status string
	if err := r.pool.QueryRow(t.Context(), `SELECT status FROM http_pipeline_deliveries WHERE id=$1`, completed.Token).Scan(&status); err != nil || status != "succeeded" {
		t.Fatalf("receipt did not settle delivery: %s %v", status, err)
	}

	sendOperator(t, r, "DELETE", "/api/trap-links/"+source.ID+"/"+target.ID, "", a, 204)
	sendOperator(t, r, "POST", "/api/trap-links", integrationJSON(t, map[string]string{"source_id": source.ID, "target_id": target.ID}), a, 201)
	if _, err := r.pool.Exec(t.Context(), `UPDATE http_pipeline_deliveries SET lease_until=$1`, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}

	if next, err := store.ClaimPipeline(targetCtx, targetIdentity, connection); err != nil || next != nil {
		t.Fatalf("revoked link retried: %+v %v", next, err)
	}
}
