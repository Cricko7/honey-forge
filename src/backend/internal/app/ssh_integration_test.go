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

func TestSSHRecordingPersistsAndRespectsOrganization(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "ssh-admin@example.test", auth.OrganizationInput{Mode: "create", Name: "SSH"})
	invite := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", admin, 200))
	viewer := registerOperator(t, r, "ssh-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: invite.Code})
	other := registerOperator(t, r, "ssh-other@example.test", auth.OrganizationInput{Mode: "create", Name: "Other"})
	config := profiles.Object{
		"services":   []any{profiles.Object{"name": "ssh", "port": 2222, "username": "root", "password": "demo-secret"}},
		"session":    profiles.Object{"max_seconds": 300, "max_connections": 4, "max_recording_bytes": 1048576, "max_upload_bytes": 1048576},
		"management": profiles.Object{"heartbeat_interval_seconds": 5, "telemetry_flush_interval_ms": 100},
	}
	w := sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, profiles.CreateRequest{RequestID: string(contract.NewID()), Name: "SSH", TypeID: "ssh-high-interaction", TypeVersion: 1, Config: config}), admin, 201)
	if strings.Contains(w.Body.String(), "demo-secret") {
		t.Fatal("profile leaked password")
	}

	p := decodeIntegration[profiles.Profile](t, w)
	trap := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "SSH", ProfileID: p.ID}), admin, 201))
	ctx, identity, connection, state := connectIntegrationAgent(t, r, admin, trap)
	revision := int32(1)
	state.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, admin, ctx, identity, "apply_config", state)
	session := string(contract.NewID())
	channel := string(contract.NewID())
	var captured []events.AgentEvent
	for i, kind := range []string{"service.connection_opened", "service.session_io", "service.connection_closed"} {
		data := `{"service":"ssh"}`
		if i == 1 {
			data = `{"service":"ssh","channel_id":"` + channel + `","stream":"stdin","payload_base64":"aWQK","bytes":3}`
		}

		if i == 2 {
			data = `{"service":"ssh","duration_ms":100,"bytes_received":3,"reason":"peer_closed"}`
		}

		captured = append(captured, events.AgentEvent{
			EventID:         string(contract.NewID()),
			EventType:       kind,
			TypeID:          "ssh-high-interaction",
			TypeVersion:     1,
			ProfileRevision: 1,
			SessionID:       session,
			SessionSequence: int64(i + 1),
			OccurredAt:      time.Now().UTC().Format(time.RFC3339Nano),
			Source:          events.Source{IP: "192.0.2.1", Port: 12345},
			Destination:     events.Destination{Protocol: "tcp", Port: 2222},
			Data:            json.RawMessage(data),
		})
	}

	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, captured...)); err != nil {
		t.Fatal(err)
	}

	path := "/api/events/recordings/" + session + "?trap_id=" + trap.ID
	record := decodeIntegration[events.Recording](t, sendOperator(t, r, "GET", path, "", viewer, 200))
	if !record.Complete || len(record.Events) != 3 || string(record.Events[1].Data) != string(captured[1].Data) {
		t.Fatalf("recording %+v", record)
	}

	sendOperator(t, r, "GET", path, "", other, 404)
	sendOperator(t, r, "GET", "/api/events/recordings/"+string(contract.NewID())+"?trap_id="+trap.ID, "", viewer, 404)
}
