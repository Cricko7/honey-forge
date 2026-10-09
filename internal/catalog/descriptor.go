package catalog

import (
	"encoding/json"
	"fmt"
	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
	"strings"

	"github.com/gin-gonic/gin/binding"
)

func compileEntry(def Definition) (*compiledEntry, error) {
	contract.Configure()
	if err := binding.Validator.ValidateStruct(def.Entry); err != nil {
		return nil, fmt.Errorf("invalid descriptor fields: %w", err)
	}
	raw, err := json.Marshal(def.Entry)
	if err != nil {
		return nil, fmt.Errorf("encode descriptor: %w", err)
	}
	// Own all nested slices/maps/raw schemas rather than sharing installation memory.
	var entry CatalogEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("decode descriptor: %w", err)
	}
	if entry.EventSchemas == nil {
		entry.EventSchemas = []EventDescriptor{}
	}
	if entry.UI.FieldOrder == nil {
		entry.UI.FieldOrder = []string{}
	}
	if entry.UI.Widgets == nil {
		entry.UI.Widgets = map[string]string{}
	}
	for _, path := range entry.UI.FieldOrder {
		if !jsonPointer(path) {
			return nil, fmt.Errorf("invalid field order pointer")
		}
	}
	for path := range entry.UI.Widgets {
		if !jsonPointer(path) {
			return nil, fmt.Errorf("invalid widget pointer")
		}
	}
	e := &compiledEntry{entry: entry, events: map[string]*configschema.Schema{}, params: map[string]*configschema.Schema{}, results: map[string]*configschema.Schema{}}
	e.config, err = compileSchema(entry.ConfigSchema)
	if err != nil {
		return nil, err
	}
	for _, event := range entry.EventSchemas {
		id := string(event.EventType)
		if e.events[id] != nil {
			return nil, fmt.Errorf("duplicate event descriptor")
		}
		schema, err := compileSchema(event.DataSchema)
		if err != nil {
			return nil, err
		}
		e.events[id] = schema
	}
	for _, action := range entry.Actions {
		id := string(action.Action)
		if e.params[id] != nil {
			return nil, fmt.Errorf("duplicate action descriptor")
		}
		params, err := compileSchema(action.ParamsSchema)
		if err != nil {
			return nil, err
		}
		result, err := compileSchema(action.ResultSchema)
		if err != nil {
			return nil, err
		}
		e.params[id] = params
		e.results[id] = result
	}
	for _, action := range []string{"start", "stop", "apply_config"} {
		if e.params[action] == nil {
			return nil, fmt.Errorf("missing base action %s", action)
		}
	}
	if def.SupportsAuthentication && e.events["service.auth_attempt"] == nil {
		return nil, fmt.Errorf("authentication runtime requires service.auth_attempt")
	}
	if def.SupportsServiceActions && e.events["service.action"] == nil {
		return nil, fmt.Errorf("service actions require service.action")
	}
	e.raw, err = json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("encode immutable descriptor: %w", err)
	}
	return e, nil
}
func compileSchema(raw json.RawMessage) (*configschema.Schema, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("schema must be an object: %w", err)
	}
	var draft string
	if err := json.Unmarshal(root["$schema"], &draft); err != nil || draft != "https://json-schema.org/draft/2020-12/schema" {
		return nil, fmt.Errorf("schema must declare Draft 2020-12")
	}
	return configschema.Compile(raw)
}
func jsonPointer(path string) bool {
	if path == "" {
		return true
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '~' {
			i++
			if i >= len(path) || path[i] != '0' && path[i] != '1' {
				return false
			}
		}
	}
	return true
}
