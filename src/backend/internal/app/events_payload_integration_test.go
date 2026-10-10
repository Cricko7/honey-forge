//go:build integration

package app

import (
	"encoding/json"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

func TestEventPayloadCaptureBudget(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "session budget"}[capture], func(t *testing.T) {
			r := openIntegrationRuntime(t)
			a := registerOperator(t, r, "payload@example.test", auth.OrganizationInput{Mode: "create", Name: "Payload"})
			config := validIntegrationConfig()
			if capture {
				config["logging"] = profiles.Object{"capture_payload": true, "max_payload_bytes": 4}
			}
			p := decodeIntegration[profiles.Profile](t, sendOperator(t, r, "POST", "/api/profiles", integrationJSON(t, profiles.CreateRequest{RequestID: string(contract.NewID()), Name: "Payload", TypeID: "tcp-banner", TypeVersion: 1, Config: config}), a, 201))
			trap := decodeIntegration[traps.Trap](t, sendOperator(t, r, "POST", "/api/traps", integrationJSON(t, traps.CreateRequest{RequestID: string(contract.NewID()), Name: "Payload", ProfileID: p.ID}), a, 201))
			ctx, identity, connection, runtime := connectIntegrationAgent(t, r, a, trap)
			revision := int32(1)
			runtime.AppliedProfileRevision = &revision
			executeIntegrationCommand(t, r, a, ctx, identity, "apply_config", runtime)
			event := attackEvent(time.Now().UTC())
			event.EventType = "tcp.payload_received"
			event.Data = json.RawMessage(`{"listener_name":"ssh","payload_base64":"YWJj","captured_bytes":3,"original_bytes":3,"truncated":false}`)
			batch := eventBatch(t, event)
			_, err := r.Agents.Ingest(ctx, identity, connection, batch)
			if !capture {
				assertEventError(t, err, "telemetry_invalid")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, event)); err != nil {
				t.Fatal("replay charged twice:", err)
			}
			event.EventID = string(contract.NewID())
			event.SessionSequence++
			_, err = r.Agents.Ingest(ctx, identity, connection, eventBatch(t, event))
			assertEventError(t, err, "telemetry_invalid")
			sendOperator(t, r, "GET", "/api/events/"+event.EventID, "", a, 404)
			// The budget applies across batches, but independent sessions may capture.
			event.SessionID = string(contract.NewID())
			if _, err := r.Agents.Ingest(ctx, identity, connection, eventBatch(t, event)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
