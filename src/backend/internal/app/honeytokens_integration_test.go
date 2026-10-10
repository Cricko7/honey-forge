//go:build integration

package app

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
	"honey-forge/internal/decoys/honeytrap"
	"honey-forge/modules/auth"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func TestHoneytokensPersistAndRespectOrganization(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "honeytoken-admin@example.test", auth.OrganizationInput{Mode: "create", Name: "Honeytokens"})
	invite := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, r, "honeytoken-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: invite.Code})
	other := registerOperator(t, r, "honeytoken-other@example.test", auth.OrganizationInput{Mode: "create", Name: "Other"})
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	config := profiles.Object{
		"services": []any{profiles.Object{"name": "web", "port": port}},
		"tokens": []any{
			profiles.Object{"id": "backup", "kind": "key", "path": "/v1/backups", "value": "fake-key-0123456789"},
			profiles.Object{"id": "document", "kind": "file", "path": "/backup.txt", "value": "fake backup"},
			profiles.Object{"id": "report", "kind": "url", "path": "/reports/latest", "value": "ready"},
		},
		"management": profiles.Object{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
	}
	request := profiles.CreateRequest{RequestID: string(contract.NewID()), Name: "Honeytokens", TypeID: "honeytoken-http", TypeVersion: 1, Config: config}
	sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, request), viewer, 403)
	w := sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, request), admin, 201)
	if strings.Contains(w.Body.String(), "fake-key") {
		t.Fatal("profile response leaked key")
	}
	p := decodeIntegration[profiles.Profile](t, w)
	sendOperator(t, r, "GET", "/api/profiles/"+p.ID, "", other, 404)
	trap := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Honeytokens", ProfileID: p.ID}), admin, 201))
	ctx, identity, connection, state := connectIntegrationAgent(t, r, admin, trap)
	revision := int32(1)
	state.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, admin, ctx, identity, "apply_config", state)
	snapshot := profiles.Snapshot{TypeID: "honeytoken-http", TypeVersion: 1, ProfileRevision: 1, Config: config}
	var captured []events.AgentEvent
	var raw []byte
	if err := r.pool.QueryRow(t.Context(), `SELECT configuration FROM commands WHERE trap_id=$1 AND action='apply_config'`, trap.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	observed := make(chan events.AgentEvent, 32)
	decoy, err := honeytrap.Start(t.Context(), snapshot, "127.0.0.1", func(_ context.Context, e events.AgentEvent) error { observed <- e; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer decoy.Stop()
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/v1/backups", "/backup.txt", "/reports/latest"} {
		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://127.0.0.1:"+strconv.Itoa(port)+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if path == "/v1/backups" {
			req.Header.Set("Authorization", "Bearer fake-key-0123456789")
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			t.Fatalf("status %d", response.StatusCode)
		}
		for range 3 {
			captured = append(captured, <-observed)
		}
	}
	batch := eventBatch(t, captured...)
	if _, err := r.Agents.Ingest(ctx, identity, connection, batch); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Agents.Ingest(ctx, identity, connection, batch); err != nil {
		t.Fatal(err)
	}
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?event_type=honeytoken.triggered", "", viewer, 200))
	if len(page.Items) != 3 {
		t.Fatalf("hits=%d", len(page.Items))
	}
	for _, e := range captured {
		sendOperator(t, r, "GET", "/api/events/"+e.EventID, "", viewer, 200)
		sendOperator(t, r, "GET", "/api/events/"+e.EventID, "", other, 404)
	}
	bad := captured[1]
	bad.EventID = string(contract.NewID())
	bad.SessionID = string(contract.NewID())
	bad.Data = json.RawMessage(`{"service":"web","token_id":"unknown","kind":"key","method":"GET"}`)
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, bad)); err == nil {
		t.Fatal("unknown token accepted")
	}
}
