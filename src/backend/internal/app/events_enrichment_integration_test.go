//go:build integration

package app

import (
	"encoding/json"
	"testing"
	"time"

	"honey-forge/modules/auth"
	"honey-forge/modules/events"
	"honey-forge/modules/traps"
)

func TestEventEnrichmentPersistedAndReplayStable(t *testing.T) {
	r := openIntegrationRuntime(t)
	a := registerOperator(t, r, "geo-owner@example.test", auth.OrganizationInput{Mode: "create", Name: "Geo"})
	_, trap, _ := createIntegrationTrap(t, r, a)
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	calls := 0
	lookup := func(ip string) *events.Enrichment {
		calls++
		if ip != "8.8.8.8" {
			t.Fatalf("lookup IP = %q", ip)
		}
		country, asn := "US", uint32(15169)
		return &events.Enrichment{CountryCode: &country, ASN: &asn}
	}
	repository := events.NewRepository(r.pool, traps.NewRepository(r.pool), r.Catalog).WithSourceLookup(lookup)
	ingester := events.NewService(repository, r.Catalog)
	event := attackEvent(time.Now().UTC().Add(-time.Minute))
	event.Source.IP = "8.8.8.8"
	batch := eventBatch(t, event)
	ack, err := ingester.Ingest(ctx, identity, connection, batch)
	if err != nil {
		t.Fatal(err)
	}
	for _, replay := range []bool{false, true} {
		if replay {
			if _, err := ingester.Ingest(ctx, identity, connection, batch); err != nil {
				t.Fatal(err)
			}
			if _, err := ingester.Ingest(ctx, identity, connection, eventBatch(t, event)); err != nil {
				t.Fatal(err)
			}
		}
		detail := decodeIntegration[events.Event](t, sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", a, 200))
		checkEnrichment(t, detail.SourceEnrichment)
		if !detail.ReceivedAt.Equal(ack.StoredAt) {
			t.Fatal("replay changed received_at")
		}
		page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", a, 200))
		if len(page.Items) != 1 {
			t.Fatalf("items: %d", len(page.Items))
		}
		checkEnrichment(t, page.Items[0].SourceEnrichment)
	}
	if calls != 1 {
		t.Fatalf("lookup called %d times; replay must keep stored enrichment", calls)
	}
	var raw []byte
	if err := r.pool.QueryRow(t.Context(), `SELECT metadata FROM mutation_changes WHERE type='event.created' AND resource_id=$1`, event.EventID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var summary events.EventSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	checkEnrichment(t, summary.SourceEnrichment)
	other := registerOperator(t, r, "geo-other@example.test", auth.OrganizationInput{Mode: "create", Name: "Other"})
	sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", other, 404)
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events", "", other, 200))
	if len(page.Items) != 0 {
		t.Fatal("another organization can see enriched events")
	}
}

func checkEnrichment(t *testing.T, got *events.Enrichment) {
	t.Helper()
	if got == nil || got.CountryCode == nil || *got.CountryCode != "US" || got.ASN == nil || *got.ASN != 15169 {
		t.Fatalf("enrichment: %+v", got)
	}
}
