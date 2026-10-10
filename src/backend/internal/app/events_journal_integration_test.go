//go:build integration

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/auth"
	"honey-forge/modules/events"
	"honey-forge/modules/traps"
)

type failingEventPublisher struct{ calls int }

func (p *failingEventPublisher) Publish(context.Context, agentws.Identity, string, []events.AgentEvent) error {
	p.calls++
	return errors.New("broker offline")
}

func TestEventJournalPinsBatchBeforeBrokerFailure(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "journal-retry@example.test", auth.OrganizationInput{Mode: "create", Name: "Journal"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	publisher := &failingEventPublisher{}
	service := events.NewJournalService(events.NewRepository(r.pool, traps.NewRepository(r.pool), r.Catalog), r.Catalog, publisher)
	event := attackEvent(time.Now().UTC())
	batch := eventBatch(t, event)
	_, err := service.Ingest(ctx, identity, connection, batch)
	assertEventError(t, err, "telemetry_unavailable")
	event.Source.Port++
	changed := eventBatch(t, event)
	changed.BatchID = batch.BatchID
	_, err = service.Ingest(ctx, identity, connection, changed)
	assertEventError(t, err, "batch_conflict")
	if publisher.calls != 1 {
		t.Fatalf("conflicting batch published: calls=%d", publisher.calls)
	}
	// A snapshot not issued to this trap must never enter the journal.
	event.ProfileRevision = 2
	_, err = service.Ingest(ctx, identity, connection, eventBatch(t, event))
	assertEventError(t, err, "telemetry_invalid")
	if publisher.calls != 1 {
		t.Fatal("invalid snapshot published")
	}
	_, err = service.Ingest(ctx, identity, connection, batch)
	assertEventError(t, err, "telemetry_unavailable")
	if publisher.calls != 2 {
		t.Fatal("identical retry blocked")
	}
}

func assertEventError(t *testing.T, err error, code string) {
	t.Helper()
	var api *contract.Error
	if !errors.As(err, &api) || api.Code != code {
		t.Fatalf("error=%v want %s", err, code)
	}
}
