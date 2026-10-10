package catalog

import (
	"context"
	"encoding/json"
	"errors"

	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
)

// ConfigSchema supplies the immutable compiled schema for secret-aware merging
// in module 04. CheckConfig must also run on the final effective configuration.
func (s *Service) ConfigSchema(ctx context.Context, id string, version contract.TypeVersion) (*configschema.Schema, error) {
	e, err := s.lookup(ctx, id, version)
	if err != nil {
		return nil, err
	}
	return e.config, nil
}

// CheckRuntimeResult additionally enforces the state-dependent rule: a stop may
// return null only before the first configuration. Module 06 supplies this fact
// from the pinned runtime state, never from the agent's result alone.
func (s *Service) CheckRuntimeResult(ctx context.Context, id string, version contract.TypeVersion, action string, result json.RawMessage, hasConfiguration bool) error {
	if err := s.CheckActionResult(ctx, id, version, action, result); err != nil {
		return err
	}
	if (id == "tcp-banner" || id == "redis-emulator") && version == 1 && hasConfiguration {
		var value struct {
			Revision *schemaInteger `json:"applied_profile_revision"`
		}
		if err := json.Unmarshal(result, &value); err != nil || value.Revision == nil {
			return contract.NewError("command_result_invalid")
		}
	}
	return nil
}

func validationError(err error, code string) error {
	if err == nil {
		return nil
	}
	result := contract.NewError(code)
	var source *contract.Error
	if errors.As(err, &source) {
		result.Fields = source.Fields
	}
	return result
}

func (s *Service) CheckConfig(ctx context.Context, id string, version contract.TypeVersion, config json.RawMessage) error {
	e, err := s.lookup(ctx, id, version)
	if err != nil {
		return err
	}
	if err := contract.CheckConfig(config); err != nil {
		return err
	}
	if err := e.config.Validate(config, "/config"); err != nil {
		return validationError(err, "config_invalid")
	}
	if id == "tcp-banner" && version == 1 {
		return checkTCPConfig(config)
	}
	return nil
}

func (s *Service) CheckEvent(ctx context.Context, id string, version contract.TypeVersion, event string, data json.RawMessage) error {
	e, err := s.lookup(ctx, id, version)
	if err != nil {
		return err
	}
	schema := e.events[event]
	if schema == nil {
		return contract.NewError("telemetry_invalid")
	}
	if err := schema.Validate(data, "/data"); err != nil {
		return validationError(err, "telemetry_invalid")
	}
	if id == "tcp-banner" && version == 1 && event == "tcp.payload_received" {
		return checkTCPPayload(data)
	}
	if event == "service.auth_attempt" || event == "service.action" {
		return checkStructuredBytes(event, data)
	}
	return nil
}

func (s *Service) CheckAction(ctx context.Context, id string, version contract.TypeVersion, action string, params json.RawMessage) (ActionDescriptor, error) {
	e, err := s.lookup(ctx, id, version)
	if err != nil {
		return ActionDescriptor{}, err
	}
	schema := e.params[action]
	if schema == nil {
		return ActionDescriptor{}, contract.NewError("unsupported_action")
	}
	if err := schema.Validate(params, "/params"); err != nil {
		return ActionDescriptor{}, validationError(err, "command_params_invalid")
	}
	entry, err := cloneEntry(e)
	if err != nil {
		return ActionDescriptor{}, err
	}
	for _, descriptor := range entry.Actions {
		if string(descriptor.Action) == action {
			return descriptor, nil
		}
	}
	return ActionDescriptor{}, contract.NewError("internal_error")
}

func (s *Service) CheckActionResult(ctx context.Context, id string, version contract.TypeVersion, action string, result json.RawMessage) error {
	e, err := s.lookup(ctx, id, version)
	if err != nil {
		return err
	}
	schema := e.results[action]
	if schema == nil {
		return contract.NewError("unsupported_action")
	}
	if err := schema.Validate(result, "/result"); err != nil {
		return validationError(err, "command_result_invalid")
	}
	if (id == "tcp-banner" || id == "redis-emulator") && version == 1 {
		var value struct {
			RuntimeState string         `json:"runtime_state"`
			Revision     *schemaInteger `json:"applied_profile_revision"`
		}
		if err := json.Unmarshal(result, &value); err != nil {
			return contract.NewError("command_result_invalid")
		}
		if value.Revision == nil && (action != "stop" || value.RuntimeState != "stopped") {
			return contract.NewError("command_result_invalid")
		}
	}
	return nil
}
