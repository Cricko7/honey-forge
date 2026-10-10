// Package audit exposes immutable, organization-scoped operator history.
package audit

import (
	"honey-forge/internal/contract"
	"time"
)

type Actor struct {
	UserID string        `json:"user_id"`
	Email  string        `json:"email"`
	Role   contract.Role `json:"role"`
}
type Resource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Details struct {
	ChangedFields        []string           `json:"changed_fields,omitempty"`
	ProfileRevision      *contract.Revision `json:"profile_revision,omitempty"`
	TrapID               string             `json:"trap_id,omitempty"`
	CommandID            string             `json:"command_id,omitempty"`
	CredentialGeneration *contract.Revision `json:"credential_generation,omitempty"`
}
type Entry struct {
	ID         string    `json:"id"`
	OccurredAt time.Time `json:"occurred_at"`
	Actor      Actor     `json:"actor"`
	Action     string    `json:"action"`
	Resource   Resource  `json:"resource"`
	Details    Details   `json:"details"`
}
type Query struct {
	Limit                       int
	Action, ActorID, ResourceID string
	From, To                    *time.Time
	Boundary                    int64
	AfterTime                   *time.Time
	AfterID                     string
}
