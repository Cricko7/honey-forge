// Package mutation coordinates durable idempotency, audit and change publication
// in the same PostgreSQL transaction as a feature's business write.
package mutation

import (
	"encoding/json"
	"honey-forge/internal/contract"
)

type Scope struct {
	OrganizationID contract.ID
	Route          string
	TrapID         contract.ID
	RequestID      contract.ID
}

// Metadata intentionally has no free-form payload, passwords, tokens or config.
type Metadata struct {
	Revision     *contract.Revision    `json:"revision,omitempty"`
	StateVersion *int64                `json:"state_version,omitempty"`
	TypeID       string                `json:"type_id,omitempty"`
	TypeVersion  *contract.TypeVersion `json:"type_version,omitempty"`
	Count        *int                  `json:"count,omitempty"`
}
type Change struct {
	Type       string
	ResourceID contract.ID
	Metadata   Metadata
}
type Outcome struct {
	ResourceID contract.ID
	Location   string
	Action     string
	Metadata   Metadata
	Changes    []Change
}
type Result struct {
	ResourceID     contract.ID
	Location       string
	StreamSequence int64
	Replayed       bool
}
type RecordedChange struct {
	Sequence   int64
	Type       string
	ResourceID contract.ID
	Metadata   json.RawMessage
	CreatedAt  contract.Timestamp
}
