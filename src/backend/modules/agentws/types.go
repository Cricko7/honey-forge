// Package agentws owns the authenticated agent control and telemetry stream.
package agentws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin/binding"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

type Identity struct {
	CredentialGeneration int64
	OrganizationID       string
	TrapID               string
	TypeID               string
	TypeVersion          int32
	RequiredActions      []string
}

type State struct {
	CurrentConfiguration     *profiles.Snapshot
	DesiredProfileRevision   *int32
	DesiredState             string
	HeartbeatIntervalSeconds int
}

// Gateway is supplied by modules 05 and 07. Ingest must commit the entire
// batch to PostgreSQL before returning a positive acknowledgement.
type Gateway interface {
	Authenticate(context.Context, string) (Identity, error)
	Observe(context.Context, Identity, string, commands.AgentRuntime) (State, error)
	Offline(context.Context, Identity, string) error
	Ingest(context.Context, Identity, string, TelemetryBatch) (TelemetryAck, error)
}

type CommandService interface {
	Claim(context.Context, string, string) (*commands.Dispatch, error)
	ExtendLease(context.Context, string, string, string, string) (time.Time, error)
	RecordResult(context.Context, string, string, commands.AgentResult) (time.Time, error)
}

type SupportedType struct {
	TypeID      string   `json:"type_id" validate:"required,type_id"`
	TypeVersion int32    `json:"type_version" validate:"min=1"`
	Actions     []string `json:"actions" validate:"min=1,dive,oneof=start stop apply_config"`
}

type AgentHello struct {
	BootID         string                `json:"boot_id" validate:"required,uuid"`
	AgentVersion   string                `json:"agent_version" validate:"required,min=1,max=64"`
	Hostname       string                `json:"hostname" validate:"required,min=1,max=255"`
	SupportedTypes []SupportedType       `json:"supported_types" validate:"min=1,max=100,dive"`
	Runtime        commands.AgentRuntime `json:"runtime" validate:"required"`
}

type Event struct {
	EventID string
	Raw     json.RawMessage
}

func (e *Event) UnmarshalJSON(raw []byte) error {
	var id struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(raw, &id); err != nil {
		return err
	}

	e.EventID = id.EventID
	e.Raw = append(e.Raw[:0], raw...)
	return nil
}

type TelemetryBatch struct {
	BatchID string  `json:"batch_id" validate:"required,uuid"`
	Events  []Event `json:"events" validate:"min=1,max=100"`
}

type TelemetryAck struct {
	BatchID              string    `json:"batch_id"`
	AcknowledgedEventIDs []string  `json:"acknowledged_event_ids"`
	StoredAt             time.Time `json:"stored_at"`
}

type progress struct {
	CommandID string `json:"command_id" validate:"required,uuid"`
	LeaseID   string `json:"lease_id" validate:"required,uuid"`
}

type heartbeat struct {
	Runtime commands.AgentRuntime `json:"runtime" validate:"required"`
}

type result struct {
	CommandID string                 `json:"command_id" validate:"required,uuid"`
	LeaseID   string                 `json:"lease_id" validate:"required,uuid"`
	Status    commands.Status        `json:"status" validate:"oneof=succeeded failed"`
	Result    json.RawMessage        `json:"result"`
	Error     *commands.RuntimeError `json:"error"`
	Runtime   commands.AgentRuntime  `json:"runtime" validate:"required"`
}

func decodePayload(payload map[string]json.RawMessage, dst any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	if err := contract.CheckJSON(encoded); err != nil {
		return contract.NewError("invalid_message")
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return contract.NewError("invalid_message")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return contract.NewError("invalid_message")
	}
	if err := binding.Validator.ValidateStruct(dst); err != nil {
		return contract.NewError("validation_failed")
	}

	return nil
}

func validateRuntime(runtime commands.AgentRuntime) error {
	if runtime.BufferedEvents < 0 || runtime.BufferBytes < 0 || runtime.BufferCapacityBytes <= 0 {
		return contract.NewError("validation_failed")
	}
	if runtime.AppliedProfileRevision != nil && *runtime.AppliedProfileRevision < 1 {
		return contract.NewError("validation_failed")
	}
	if runtime.RuntimeState != "running" && runtime.RuntimeState != "stopped" && runtime.RuntimeState != "error" && runtime.RuntimeState != "unknown" {
		return contract.NewError("validation_failed")
	}
	if runtime.BufferState != "ok" && runtime.BufferState != "full" && runtime.BufferState != "unavailable" {
		return contract.NewError("validation_failed")
	}
	if runtime.RuntimeState == "error" && runtime.LastError == nil {
		return contract.NewError("validation_failed")
	}
	if runtime.LastError != nil && (runtime.LastError.Code == "" || len(runtime.LastError.Message) > 200) {
		return contract.NewError("validation_failed")
	}
	if runtime.LastError != nil && commands.NormalizeRuntimeError(runtime.LastError) != nil {
		return contract.NewError("validation_failed")
	}

	return nil
}

func validateHello(identity Identity, hello AgentHello) error {
	if err := validateRuntime(hello.Runtime); err != nil {
		return err
	}

	found := false
	seen := make(map[string]bool, len(hello.SupportedTypes))
	for _, supported := range hello.SupportedTypes {
		key := fmt.Sprintf("%s/%d", supported.TypeID, supported.TypeVersion)
		if seen[key] {
			return contract.NewError("validation_failed")
		}
		seen[key] = true

		actions := make(map[string]bool, len(supported.Actions))
		for _, action := range supported.Actions {
			if actions[action] {
				return contract.NewError("validation_failed")
			}
			actions[action] = true
		}

		if supported.TypeID == identity.TypeID && supported.TypeVersion == identity.TypeVersion {
			found = true
			for _, action := range identity.RequiredActions {
				if !actions[action] {
					return contract.NewError("unsupported_type")
				}
			}
		}
	}
	if !found {
		return contract.NewError("unsupported_type")
	}

	return nil
}

func validateBatch(batch TelemetryBatch) error {
	if !contract.ValidID(batch.BatchID) || len(batch.Events) < 1 || len(batch.Events) > 100 {
		return contract.NewError("validation_failed")
	}

	seen := make(map[string]bool, len(batch.Events))
	for _, event := range batch.Events {
		if !contract.ValidID(event.EventID) || len(event.Raw) > 16*1024 || seen[event.EventID] {
			return contract.NewError("validation_failed")
		}
		seen[event.EventID] = true
	}

	return nil
}

func validateResultPayload(payload map[string]json.RawMessage) error {
	if _, ok := payload["result"]; !ok {
		return contract.NewError("command_result_invalid")
	}
	if _, ok := payload["error"]; !ok {
		return contract.NewError("command_result_invalid")
	}

	return nil
}
