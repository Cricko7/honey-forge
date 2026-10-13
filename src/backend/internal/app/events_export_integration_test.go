//go:build integration

package app

import (
	"encoding/csv"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"honey-forge/modules/auth"
	"honey-forge/modules/events"
)

func TestEventFiltersAndExport(t *testing.T) {
	r := openIntegrationRuntime(t)
	admin := registerOperator(t, r, "export@example.test", auth.OrganizationInput{Mode: "create", Name: "Export"})
	foreign := registerOperator(t, r, "foreign-export@example.test", auth.OrganizationInput{Mode: "create", Name: "Foreign"})
	_, trap, _ := createIntegrationTrap(t, r, admin)
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, admin, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, admin, ctx, identity, "apply_config", runtime)
	at := time.Date(2000, 1, 1, 0, 0, 0, 123456789, time.UTC)
	first, second := attackEvent(at), attackEvent(at.Add(time.Nanosecond))
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, first, second)); err != nil {
		t.Fatal(err)
	}

	query := url.Values{
		"trap_id": {trap.ID}, "type_id": {"tcp-banner"},
		"event_type": {first.EventType},
		"from":       {at.Format(time.RFC3339Nano)}, "to": {at.Add(time.Nanosecond).Format(time.RFC3339Nano)},
	}
	page := decodeIntegration[events.EventPage](t, sendOperator(t, r, "GET", "/api/events?"+query.Encode(), "", admin, 200))
	if len(page.Items) != 1 || page.Items[0].EventID != first.EventID {
		t.Fatalf("filtered page: %+v", page)
	}

	query.Set("format", "csv")
	csvResponse := sendOperator(t, r, "GET", "/api/events/export?"+query.Encode(), "", admin, 200)
	rows, err := csv.NewReader(strings.NewReader(csvResponse.Body.String())).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][0] != first.EventID || rows[1][15] != string(first.Data) {
		t.Fatalf("CSV: %+v, %v", rows, err)
	}

	query.Set("format", "stix")
	stixResponse := sendOperator(t, r, "GET", "/api/events/export?"+query.Encode(), "", admin, 200)
	var bundle struct{ Objects []map[string]json.RawMessage }
	if err := json.Unmarshal(stixResponse.Body.Bytes(), &bundle); err != nil || len(bundle.Objects) != 2 {
		t.Fatalf("STIX: %s, %v", stixResponse.Body.String(), err)
	}

	sendOperator(t, r, "GET", "/api/events/export?"+query.Encode(), "", foreign, 404)
	sendOperator(t, r, "GET", "/api/events/export?format=csv", "", operatorSession{}, 401)
	query.Set("type_id", "redis-emulator")
	query.Set("format", "csv")
	empty := sendOperator(t, r, "GET", "/api/events/export?"+query.Encode(), "", admin, 200)
	rows, err = csv.NewReader(strings.NewReader(empty.Body.String())).ReadAll()
	if err != nil || len(rows) != 1 {
		t.Fatalf("empty CSV: %+v, %v", rows, err)
	}

	foreignEmpty := sendOperator(t, r, "GET", "/api/events/export?format=stix", "", foreign, 200)
	if strings.Contains(foreignEmpty.Body.String(), first.EventID) {
		t.Fatal("foreign organization exported attack data")
	}
}
