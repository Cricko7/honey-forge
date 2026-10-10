package agentws

import (
	"encoding/json"
	"strings"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
)

func TestValidateHello(t *testing.T) {
	identity := Identity{OrganizationID: testOrgID, TrapID: testTrapID, TypeID: "tcp-banner", TypeVersion: 1, RequiredActions: []string{"start", "stop"}}
	base := AgentHello{
		BootID: string(contract.NewID()), AgentVersion: "0.1.0", Hostname: "decoy-01",
		SupportedTypes: []SupportedType{{TypeID: "tcp-banner", TypeVersion: 1, Actions: []string{"start", "stop"}}},
		Runtime:        commands.AgentRuntime{RuntimeState: "stopped", BufferState: "ok", BufferCapacityBytes: 1024},
	}

	for _, tt := range []struct {
		name   string
		change func(*AgentHello)
		valid  bool
	}{
		{"valid", func(*AgentHello) {}, true},
		{"unsupported version", func(h *AgentHello) { h.SupportedTypes[0].TypeVersion = 2 }, false},
		{"duplicate type", func(h *AgentHello) { h.SupportedTypes = append(h.SupportedTypes, h.SupportedTypes[0]) }, false},
		{"duplicate action", func(h *AgentHello) { h.SupportedTypes[0].Actions = []string{"stop", "stop"} }, false},
		{"missing required action", func(h *AgentHello) { h.SupportedTypes[0].Actions = []string{"stop"} }, false},
		{"negative buffer", func(h *AgentHello) { h.Runtime.BufferBytes = -1 }, false},
		{"error without detail", func(h *AgentHello) { h.Runtime.RuntimeState = "error" }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hello := base
			hello.SupportedTypes = append([]SupportedType(nil), base.SupportedTypes...)
			tt.change(&hello)
			if (validateHello(identity, hello) == nil) != tt.valid {
				t.Fatal("unexpected validation outcome")
			}
		})
	}
}

func TestTelemetryQuarantineRuntime(t *testing.T) {
	runtime := commands.AgentRuntime{RuntimeState: "stopped", BufferState: "ok", BufferedEvents: 1, BufferCapacityBytes: 1024, LastError: &commands.RuntimeError{Code: "telemetry_invalid", Message: "captured secret"}}
	if err := validateRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.LastError.Message != "Telemetry event is invalid" {
		t.Fatal("unsafe runtime error")
	}
}

func TestValidateBatch(t *testing.T) {
	id := string(contract.NewID())
	other := string(contract.NewID())
	for _, tt := range []struct {
		name   string
		events []Event
		valid  bool
	}{
		{"one event", []Event{{EventID: id, Raw: json.RawMessage(`{"event_id":"` + id + `"}`)}}, true},
		{"empty", nil, false},
		{"duplicate event", []Event{{EventID: id}, {EventID: id}}, false},
		{"invalid id", []Event{{EventID: "bad"}}, false},
		{"oversize event", []Event{{EventID: other, Raw: json.RawMessage(strings.Repeat("x", 16*1024+1))}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if (validateBatch(TelemetryBatch{BatchID: id, Events: tt.events}) == nil) != tt.valid {
				t.Fatal("unexpected validation outcome")
			}
		})
	}
}

func TestValidateResultPayload(t *testing.T) {
	for _, tt := range []struct {
		name    string
		payload map[string]json.RawMessage
		valid   bool
	}{
		{"both present", map[string]json.RawMessage{"result": json.RawMessage(`null`), "error": json.RawMessage(`null`)}, true},
		{"missing result", map[string]json.RawMessage{"error": json.RawMessage(`null`)}, false},
		{"missing error", map[string]json.RawMessage{"result": json.RawMessage(`{}`)}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if (validateResultPayload(tt.payload) == nil) != tt.valid {
				t.Fatal("unexpected validation outcome")
			}
		})
	}
}
