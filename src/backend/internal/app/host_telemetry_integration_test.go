//go:build integration

package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

// This is a real PostgreSQL/API test with a synthetic normalized sensor event,
// not proof of eBPF collection. The Linux sensor acceptance is separate.
func TestHostTelemetryPersistsAndRespectsOrganization(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "host-admin@example.test", auth.OrganizationInput{Mode: "create", Name: "Host telemetry"})
	other := registerOperator(t, r, "host-other@example.test", auth.OrganizationInput{Mode: "create", Name: "Other"})
	config := profiles.Object{
		"services":   []any{profiles.Object{"name": "ssh", "port": 2222, "username": "root", "password": "demo-secret"}},
		"session":    profiles.Object{"max_seconds": 300, "max_connections": 4, "max_recording_bytes": 1048576, "max_upload_bytes": 1048576},
		"management": profiles.Object{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
	}
	p := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, profiles.CreateRequest{RequestID: string(contract.NewID()), Name: "SSH host", TypeID: "ssh-high-interaction", TypeVersion: 2, Config: config}), admin, 201))
	trap := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "SSH host", ProfileID: p.ID}), admin, 201))
	ctx, identity, connection, state := connectIntegrationAgent(t, r, admin, trap)
	revision := int32(1)
	state.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, admin, ctx, identity, "apply_config", state)
	at := time.Now().UTC()
	event := attackEvent(at)
	event.TypeID, event.TypeVersion, event.EventType = "ssh-high-interaction", 2, "process.exec"
	event.Data = json.RawMessage(integrationJSON(t, map[string]any{
		"service": "ssh", "schema_version": 1, "sensor": "tetragon", "sensor_version": "1.7.0",
		"host_id": "test-node", "generation": identity.CredentialGeneration,
		"container_id": strings.Repeat("a", 64), "container_started_at": at.Add(-time.Second).Format(time.RFC3339Nano),
		"pid": 2345, "ppid": nil, "uid": 1000, "binary": "/usr/bin/id", "cwd": "/home/sandbox", "arguments_redacted": true,
	}))
	batch := eventBatch(t, event)
	for range 2 {
		if _, err := r.Agents.Ingest(ctx, identity, connection, batch); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, event)); err != nil {
		t.Fatal("same event in another batch rejected:", err)
	}
	detail := decodeIntegration[events.Event](t, sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", admin, 200))
	if detail.TrapID != trap.ID || detail.ReceivedAt.IsZero() || detail.OccurredAt != event.OccurredAt || string(detail.Data) != string(event.Data) {
		t.Fatalf("stored event changed: %+v", detail)
	}
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?trap_id="+trap.ID+"&event_type=process.exec", "", admin, 200))
	if len(page.Items) != 1 {
		t.Fatalf("duplicate persisted: %+v", page)
	}
	sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", other, 404)
	sendOperator(t, r, "GET", "/api/events/recordings/"+event.SessionID+"?trap_id="+trap.ID, "", other, 404)
}
