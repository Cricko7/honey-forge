// Package traps owns registrations, runtime observations and agent credentials.
package traps

import (
	"time"

	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

type CreateRequest struct {
	RequestID   string `json:"request_id" validate:"required,uuid"`
	Name        string `json:"name" validate:"required,name"`
	Description string `json:"description" validate:"max=1000"`
	ProfileID   string `json:"profile_id" validate:"required,uuid"`
}
type PatchRequest struct {
	Name        *string `json:"name" validate:"omitempty,name"`
	Description *string `json:"description" validate:"omitempty,max=1000"`
}
type CredentialsRequest struct {
	ExpectedGeneration *int64 `json:"expected_generation" validate:"required,min=0,max=2147483646"`
}
type RuntimeError = commands.RuntimeError
type AgentStatus struct {
	AgentVersion        string        `json:"agent_version"`
	Hostname            string        `json:"hostname"`
	BufferedEvents      int64         `json:"buffered_events"`
	BufferBytes         int64         `json:"buffer_bytes"`
	BufferCapacityBytes int64         `json:"buffer_capacity_bytes"`
	BufferState         string        `json:"buffer_state"`
	LastError           *RuntimeError `json:"last_error"`
}
type Trap struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	Description            string       `json:"description"`
	ProfileID              string       `json:"profile_id"`
	TypeID                 string       `json:"type_id"`
	TypeVersion            int32        `json:"type_version"`
	InteractionLevel       string       `json:"interaction_level"`
	Revision               int64        `json:"revision"`
	StateVersion           int64        `json:"state_version"`
	CreatedAt              time.Time    `json:"created_at"`
	UpdatedAt              time.Time    `json:"updated_at"`
	Connectivity           string       `json:"connectivity"`
	LastSeenAt             *time.Time   `json:"last_seen_at"`
	RuntimeState           string       `json:"runtime_state"`
	DesiredState           string       `json:"desired_state"`
	DesiredProfileRevision *int32       `json:"desired_profile_revision"`
	AppliedProfileRevision *int32       `json:"applied_profile_revision"`
	Agent                  *AgentStatus `json:"agent"`
	ActiveCommandID        *string      `json:"active_command_id"`
}
type AgentCredentials struct {
	TrapID     string    `json:"trap_id"`
	Token      string    `json:"token"`
	Generation int64     `json:"generation"`
	AgentWSURL string    `json:"agent_ws_url"`
	IssuedAt   time.Time `json:"issued_at"`
}
type CredentialsStatus struct {
	TrapID     string     `json:"trap_id"`
	Generation int64      `json:"generation"`
	Active     bool       `json:"active"`
	IssuedAt   *time.Time `json:"issued_at"`
}

// Record is internal storage state. Only Trap is an operator DTO.
type Record struct {
	Trap
	OrganizationID       string
	Generation           int64
	TokenHash            []byte
	IssuedAt             *time.Time
	ConnectionID         *string
	PendingIngestions    int64
	AppliedConfiguration *profiles.Snapshot
}
type CreateResult struct {
	Trap     Trap
	Replayed bool
}
type ListQuery struct {
	Limit                   int
	ProfileID, Connectivity string
	Boundary                time.Time
	BoundaryID              string
	AfterTime               time.Time
	AfterID                 string
}
