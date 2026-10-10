//go:build integration

package app

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/auth"
	"honey-forge/modules/events"
)

func eventBatch(t *testing.T, values ...events.AgentEvent) agentws.TelemetryBatch {
	t.Helper()
	b := agentws.TelemetryBatch{BatchID: string(contract.NewID())}
	for _, v := range values {
		b.Events = append(b.Events, agentws.Event{EventID: v.EventID, Raw: json.RawMessage(integrationJSON(t, v))})
	}
	return b
}

func attackEvent(at time.Time) events.AgentEvent {
	return events.AgentEvent{EventID: string(contract.NewID()), EventType: "tcp.connection_opened", TypeID: "tcp-banner", TypeVersion: 1, ProfileRevision: 1, OccurredAt: at.Format(time.RFC3339Nano), SessionID: string(contract.NewID()), SessionSequence: 1, Source: events.Source{IP: "2001:0db8::1", Port: 50000}, Destination: events.Destination{Protocol: "tcp", Port: 2222}, Data: json.RawMessage(`{"listener_name":"ssh"}`)}
}

func TestEventAtomicConflicts(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "event-conflicts@example.test", auth.OrganizationInput{Mode: "create", Name: "Events"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	original := attackEvent(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	b := eventBatch(t, original)
	ack, err := r.Agents.Ingest(ctx, identity, connection, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, code string
		change     func(*agentws.TelemetryBatch, *events.AgentEvent)
	}{
		{"event content conflict", "event_id_conflict", func(_ *agentws.TelemetryBatch, e *events.AgentEvent) { e.EventID = original.EventID; e.Source.Port++ }},
		{"batch content conflict", "batch_conflict", func(b *agentws.TelemetryBatch, _ *events.AgentEvent) { b.BatchID = ack.BatchID }},
		{"session sequence conflict", "telemetry_invalid", func(_ *agentws.TelemetryBatch, e *events.AgentEvent) { e.SessionID = original.SessionID }},
		{"unissued revision", "telemetry_invalid", func(_ *agentws.TelemetryBatch, e *events.AgentEvent) { e.ProfileRevision = 2 }},
		{"wrong listener", "telemetry_invalid", func(_ *agentws.TelemetryBatch, e *events.AgentEvent) { e.Destination.Port = 2223 }},
		{"unsupported event", "telemetry_invalid", func(_ *agentws.TelemetryBatch, e *events.AgentEvent) { e.EventType = "service.action" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := attackEvent(time.Now().UTC().Add(-time.Hour))
			bad := attackEvent(time.Now().UTC().Add(-time.Hour))
			batch := eventBatch(t, fresh, bad)
			tc.change(&batch, &bad)
			batch.Events[1] = eventBatch(t, bad).Events[0]
			_, err := r.Agents.Ingest(ctx, identity, connection, batch)
			var api *contract.Error
			if !errors.As(err, &api) || api.Code != tc.code {
				t.Fatalf("err=%v want %s", err, tc.code)
			}
			sendOperator(t, r, "GET", "/api/events/"+fresh.EventID, "", a, 404)
		})
	}
	// A new batch containing an identical event is a replay, including normalized IP/time.
	normalized := original
	normalized.Source.IP = "2001:db8::1"
	normalized.OccurredAt = "2000-01-01T03:00:00+03:00"
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, normalized)); err != nil {
		t.Fatal(err)
	}
	detail := decodeIntegration[events.Event](t, sendOperator(t, r, "GET", "/api/events/"+original.EventID, "", a, 200))
	if !detail.ReceivedAt.Equal(ack.StoredAt) || detail.SourceEnrichment == nil || detail.SourceEnrichment.ASN != nil || detail.SourceEnrichment.CountryCode != nil {
		t.Fatal("replay timestamp or enrichment changed")
	}
	var count int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM mutation_changes WHERE type='event.created' AND resource_id=$1`, original.EventID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("notifications=%d err=%v", count, err)
	}
	var metadata map[string]json.RawMessage
	var raw []byte
	if err := r.pool.QueryRow(t.Context(), `SELECT metadata FROM mutation_changes WHERE type='event.created' AND resource_id=$1`, original.EventID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if _, ok := metadata["data"]; ok || len(metadata) != 12 {
		t.Fatalf("unsafe/incomplete summary: %s", raw)
	}
}

func TestEventSnapshotPagination(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "event-pages@example.test", auth.OrganizationInput{Mode: "create", Name: "Pages"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	at := time.Date(2000, 1, 1, 0, 0, 0, 123456789, time.UTC)
	first, second, third := attackEvent(at), attackEvent(at.Add(-time.Nanosecond)), attackEvent(at.Add(-time.Second))
	first.EventID = "11111111-1111-4111-8111-111111111111"
	second.EventID = "22222222-2222-4222-8222-222222222222"
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, third, first, second)); err != nil {
		t.Fatal(err)
	}
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?limit=1", "", a, 200))
	if len(page.Items) != 1 || page.Items[0].EventID != first.EventID || page.NextCursor == nil || page.StreamCursor == "" {
		t.Fatalf("first page: %+v", page)
	}
	late := attackEvent(at.Add(-time.Minute))
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, late)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{second.EventID, third.EventID} {
		if page.NextCursor == nil {
			t.Fatal("cursor missing")
		}
		next := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?limit=1&cursor="+url.QueryEscape(*page.NextCursor), "", a, 200))
		if len(next.Items) != 1 || next.Items[0].EventID != want || next.StreamCursor != page.StreamCursor {
			t.Fatalf("next page: %+v want %s", next, want)
		}
		page = next
	}
	if page.NextCursor != nil {
		t.Fatal("late event entered original snapshot")
	}
	filtered := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?trap_id="+trap.ID+"&source_ip=2001:db8::1&session_id="+second.SessionID+"&event_type=tcp.connection_opened&from=1999-01-01T00:00:00Z&to=2001-01-01T00:00:00Z", "", a, 200))
	if len(filtered.Items) != 1 || filtered.Items[0].EventID != second.EventID {
		t.Fatal(filtered)
	}
	precise := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?from="+url.QueryEscape(second.OccurredAt)+"&to="+url.QueryEscape(first.OccurredAt), "", a, 200))
	if len(precise.Items) != 1 || precise.Items[0].EventID != second.EventID {
		t.Fatalf("nanosecond range: %+v", precise)
	}
	empty := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?event_type=service.unknown", "", a, 200))
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil || empty.StreamCursor == "" {
		t.Fatal(empty)
	}
	refreshed := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", a, 200))
	if len(refreshed.Items) != 4 {
		t.Fatal(refreshed)
	}
	foreign := registerOperator(t, r, "event-page-foreign@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	initial := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?limit=1", "", a, 200))
	sendOperator(t, r, "GET", "/api/events?cursor="+url.QueryEscape(*initial.NextCursor), "", foreign, 400)
	sendOperator(t, r, "GET", "/api/events?cursor="+url.QueryEscape(initial.StreamCursor), "", a, 400)
	sendOperator(t, r, "GET", "/api/events?trap_id="+trap.ID, "", foreign, 404)
	sendOperator(t, r, "GET", "/api/events/"+first.EventID, "", operatorSession{}, 401)
}
