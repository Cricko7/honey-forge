//go:build integration

package app

import (
	"encoding/json"
	"sync"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func TestNetworkPipelineFollowsLinksWithoutReplayingDiscardedTraffic(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "network-pipeline@example.test", auth.OrganizationInput{Mode: "create", Name: "Network pipeline"})
	config := profiles.Object{
		"services": []any{map[string]any{"name": "web", "port": 32089, "entry": "web"}},
		"blocks": []any{
			map[string]any{"id": "web", "kind": "input", "config": map[string]any{"name": "web", "port": 32089, "method": "POST", "path": "/pipeline"}, "outputs": map[string]string{"out": "send"}},
			map[string]any{"id": "send", "kind": "send", "config": map[string]any{"target_id": pipeline.NetworkTargetID, "service": "web", "method": "POST", "path": "/pipeline", "payload": `"${json}"`}, "outputs": map[string]string{"out": "reply"}},
			map[string]any{"id": "reply", "kind": "response", "config": map[string]any{"status": 202, "format": "json", "body": "{}", "headers": "{}"}, "outputs": map[string]string{}},
		},
		"limits":     map[string]any{"max_steps": 512, "timeout_ms": 10000, "max_body_bytes": 65536},
		"management": map[string]any{"heartbeat_interval_seconds": 10, "telemetry_flush_interval_ms": 500},
	}
	profile := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, profiles.CreateRequest{RequestID: string(contract.NewID()), Name: "Network", TypeID: "custom-http", TypeVersion: 2, Config: config}), a, 201))
	create := func(name string) traps.Trap {
		return decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: name, ProfileID: profile.ID}), a, 201))
	}
	source, first, second := create("Source"), create("First"), create("Second")
	ctx, identity, _, runtime := connectIntegrationAgent(t, r, a, source)
	var revision int32 = 1
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	runtime.RuntimeState = "running"
	executeIntegrationCommand(t, r, a, ctx, identity, "start", runtime)
	firstCtx, firstIdentity, firstConnection, firstRuntime := connectIntegrationAgent(t, r, a, first)
	firstRuntime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, firstCtx, firstIdentity, "apply_config", firstRuntime)
	firstRuntime.RuntimeState = "running"
	executeIntegrationCommand(t, r, a, firstCtx, firstIdentity, "start", firstRuntime)
	secondCtx, secondIdentity, secondConnection, secondRuntime := connectIntegrationAgent(t, r, a, second)
	secondRuntime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, secondCtx, secondIdentity, "apply_config", secondRuntime)
	secondRuntime.RuntimeState = "running"
	executeIntegrationCommand(t, r, a, secondCtx, secondIdentity, "start", secondRuntime)

	store := traps.NewRepository(r.pool)
	request := pipeline.Request{RequestID: string(contract.NewID()), NodeID: "send", TargetID: pipeline.NetworkTargetID, Service: "web", Method: "POST", Path: "/pipeline", Payload: json.RawMessage(`{"run":"network"}`), SourceIP: "127.0.0.1", SourcePort: 1234}
	assertCount := func(want int) {
		t.Helper()
		var count int
		if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM http_pipeline_deliveries WHERE source_id=$1`, source.ID).Scan(&count); err != nil || count != want {
			t.Fatalf("deliveries=%d, want=%d: %v", count, want, err)
		}
	}
	if err := store.EnqueuePipeline(ctx, identity, request); err != nil {
		t.Fatalf("unconnected send must succeed: %v", err)
	}
	assertCount(0)

	for _, target := range []traps.Trap{first, second} {
		sendOperator(t, r, "POST", "/api/trap-links", integrationJSON(t, map[string]string{"source_id": source.ID, "target_id": target.ID}), a, 201)
	}
	if err := store.EnqueuePipeline(ctx, identity, request); err != nil {
		t.Fatal(err)
	}
	assertCount(0) // An acknowledged discard cannot be replayed into newly drawn arrows.

	request.RequestID = string(contract.NewID())
	var group sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		group.Go(func() { results <- store.EnqueuePipeline(ctx, identity, request) })
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertCount(2)
	if delivery, err := store.ClaimPipeline(firstCtx, firstIdentity, firstConnection); err != nil || delivery == nil || delivery.TargetID != first.ID || string(delivery.Payload) != string(request.Payload) {
		t.Fatalf("first receiver: %+v %v", delivery, err)
	}

	changed := request
	changed.Payload = json.RawMessage(`{"run":"changed"}`)
	if err := store.EnqueuePipeline(ctx, identity, changed); err == nil {
		t.Fatal("changed payload reused request identity")
	}
	foreign := identity
	foreign.OrganizationID = string(contract.NewID())
	if err := store.EnqueuePipeline(ctx, foreign, request); err == nil {
		t.Fatal("foreign organization routed traffic")
	}

	sendOperator(t, r, "DELETE", "/api/trap-links/"+source.ID+"/"+second.ID, "", a, 204)
	if delivery, err := store.ClaimPipeline(secondCtx, secondIdentity, secondConnection); err != nil || delivery != nil {
		t.Fatalf("deleted arrow delivered queued traffic: %+v %v", delivery, err)
	}
	request.RequestID = string(contract.NewID())
	if err := store.EnqueuePipeline(ctx, identity, request); err != nil {
		t.Fatal(err)
	}
	assertCount(3)

	// A failing receiver must roll back the entire fan-out, including earlier valid receivers.
	sendOperator(t, r, "POST", "/api/trap-links", integrationJSON(t, map[string]string{"source_id": source.ID, "target_id": second.ID}), a, 201)
	bad := second.ID
	if first.ID > second.ID {
		bad = first.ID
	}
	if _, err := r.pool.Exec(t.Context(), `UPDATE traps SET applied_configuration=jsonb_set(applied_configuration,'{config,blocks,0,config,path}','"/other"'::jsonb) WHERE id=$1`, bad); err != nil {
		t.Fatal(err)
	}
	request.RequestID = string(contract.NewID())
	if err := store.EnqueuePipeline(ctx, identity, request); err == nil {
		t.Fatal("incompatible network receiver accepted")
	}
	assertCount(3)
}
