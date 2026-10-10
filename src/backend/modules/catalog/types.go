// Package catalog owns immutable type descriptors and validation for modules 04/06/07.
package catalog

import (
	"encoding/json"

	"honey-forge/internal/contract"
)

type CatalogEntry struct {
	TypeID                  contract.TypeID      `json:"type_id" validate:"required,type_id"`
	TypeVersion             contract.TypeVersion `json:"type_version" validate:"min=1,max=2147483647"`
	Title                   string               `json:"title" validate:"min=1,max=100"`
	Description             contract.Description `json:"description" validate:"max=1000"`
	InteractionLevel        string               `json:"interaction_level" validate:"min=1,max=32"`
	AvailableForNewProfiles bool                 `json:"available_for_new_profiles"`
	ConfigSchema            json.RawMessage      `json:"config_schema"`
	EventSchemas            []EventDescriptor    `json:"event_schemas" validate:"dive"`
	Actions                 []ActionDescriptor   `json:"actions" validate:"min=3,dive"`
	UI                      UIHints              `json:"ui"`
}

type EventDescriptor struct {
	EventType  contract.EventType `json:"event_type" validate:"required,type_id"`
	Title      string             `json:"title" validate:"min=1,max=100"`
	DataSchema json.RawMessage    `json:"data_schema"`
}

type ActionDescriptor struct {
	Action       contract.Action `json:"action" validate:"required,type_id"`
	Title        string          `json:"title" validate:"min=1,max=100"`
	ParamsSchema json.RawMessage `json:"params_schema"`
	ResultSchema json.RawMessage `json:"result_schema"`
}

type UIHints struct {
	FieldOrder []string          `json:"field_order"`
	Widgets    map[string]string `json:"widgets" validate:"dive,oneof=text textarea number checkbox select password array object"`
}

// Definition is trusted installation metadata, never an operator request.
// Capabilities require the matching service events even if payload capture is disabled.
type Definition struct {
	Entry                  CatalogEntry
	SupportsAuthentication bool
	SupportsServiceActions bool
}

type Query struct {
	Limit     int
	Cursor    string
	TypeID    string
	Available *bool
}
