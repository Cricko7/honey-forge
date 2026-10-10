//go:build integration

package app

import (
	"encoding/json"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func TestStructuredEventHistory(t *testing.T) {
	// A fixture descriptor proves the generic backend contract without claiming
	// that a Medium runtime is shipped or changing tcp-banner/1's immutable schema.
	defs := catalog.BuiltinDefinitions()
	defs[0].Entry.TypeID = "service-demo"
	defs[0].Entry.EventSchemas = catalog.StructuredEventSchemas()
	defs[0].SupportsAuthentication = true
	defs[0].SupportsServiceActions = true
	defs[0].Entry.ConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["services","logging"],"properties":{"services":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","minLength":1,"maxLength":64}}}},"logging":{"type":"object","additionalProperties":false,"required":["capture_payload"],"properties":{"capture_payload":{"type":"boolean"}}}}}`)
	r := openIntegrationRuntimeWithDefinitions(t, defs)
	a := registerOperator(t, r, "captured@example.test", auth.OrganizationInput{Mode: "create", Name: "Captured"})
	invite := decodeIntegration[auth.JoinCode](t, sendOperator(t, r, "GET", "/api/organization/join-code", "", a, 200))
	viewer := registerOperator(t, r, "captured-viewer@example.test", auth.OrganizationInput{Mode: "join", JoinCode: invite.Code})
	p := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, profiles.CreateRequest{RequestID: string(contract.NewID()), Name: "Service", TypeID: "service-demo", TypeVersion: 1, Config: profiles.Object{"services": []any{profiles.Object{"name": "ssh"}}, "logging": profiles.Object{"capture_payload": false}}}), a, 201))
	trap := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Service", ProfileID: p.ID}), a, 201))
	ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
	revision := int32(1)
	runtime.AppliedProfileRevision = &revision
	executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
	password := " <script>attacker</script> \nПароль \x00 "
	input := " GET /a?x=<script> HTTP/1.1\n\nтело \x00 "
	authEvent := attackEvent(time.Now().UTC())
	authEvent.TypeID = "service-demo"
	authEvent.EventType = "service.auth_attempt"
	authEvent.Data = json.RawMessage(integrationJSON(t, map[string]any{"service": "ssh", "username": " Root ", "password": password, "outcome": "rejected", "truncated": false}))
	actionEvent := authEvent
	actionEvent.EventID = string(contract.NewID())
	actionEvent.EventType = "service.action"
	actionEvent.SessionSequence = 2
	actionEvent.Data = json.RawMessage(integrationJSON(t, map[string]any{"service": "ssh", "action_kind": "request", "input": input, "outcome": "accepted", "truncated": false}))
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, actionEvent, authEvent)); err != nil {
		t.Fatal(err)
	}
	for _, session := range []operatorSession{a, viewer} {
		for _, event := range []events.AgentEvent{authEvent, actionEvent} {
			detail := decodeIntegration[events.Event](t, sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", session, 200))
			var data map[string]any
			if err := json.Unmarshal(detail.Data, &data); err != nil {
				t.Fatal(err)
			}
			if event.EventType == "service.auth_attempt" && (data["username"] != " Root " || data["password"] != password) || event.EventType == "service.action" && data["input"] != input {
				t.Fatal("captured values changed")
			}
		}
	}
	// Updating/deleting the source profile cannot rewrite the issued snapshot or data.
	sendOperator(t, r, "PATCH", "/api/profiles/"+p.ID, `{"config":{"services":[{"name":"http"}],"logging":{"capture_payload":false}}}`, a, 200, `"profile:`+p.ID+`:1"`)
	old := authEvent
	old.EventID = string(contract.NewID())
	old.SessionSequence = 3
	if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, old)); err != nil {
		t.Fatal("previous issued revision rejected:", err)
	}
	trapMutation(t, r, a, "DELETE", "/api/traps/"+trap.ID, "", 1, 204)
	sendOperator(t, r, "DELETE", "/api/profiles/"+p.ID, "", a, 204, `"profile:`+p.ID+`:2"`)
	sendOperator(t, r, "GET", "/api/events/"+authEvent.EventID, "", viewer, 200)
	var unsafe int
	if err := r.pool.QueryRow(t.Context(), `SELECT count(*) FROM mutation_audit WHERE metadata::text LIKE '%attacker%' OR metadata::text LIKE '%Пароль%'`).Scan(&unsafe); err != nil || unsafe != 0 {
		t.Fatalf("unsafe audit=%d err=%v", unsafe, err)
	}
	page := sendOperator(t, r, "GET", "/api/events", "", viewer, 200)
	var wire struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(page.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	for _, item := range wire.Items {
		if _, ok := item["data"]; ok {
			t.Fatal("summary exposed captured data")
		}
	}
}
