// Package commands owns operator commands and their pinned configuration.
package commands

import (
	"context"
	"encoding/json"
	"time"

	"honey-forge/modules/profiles"
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Expired   Status = "expired"
)

type CreateRequest struct {
	RequestID string          `json:"request_id" binding:"required,uuid" validate:"required,uuid"`
	Action    string          `json:"action" binding:"required,max=64" validate:"required,max=64"`
	Params    json.RawMessage `json:"params" binding:"required" validate:"required"`
}

type RuntimeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Command struct {
	ID                    string          `json:"id"`
	TrapID                string          `json:"trap_id"`
	RequestID             string          `json:"request_id"`
	Action                string          `json:"action"`
	Params                json.RawMessage `json:"params"`
	Status                Status          `json:"status"`
	TargetProfileRevision *int32          `json:"target_profile_revision"`
	CreatedAt             time.Time       `json:"created_at"`
	ExpiresAt             time.Time       `json:"expires_at"`
	StartedAt             *time.Time      `json:"started_at"`
	FinishedAt            *time.Time      `json:"finished_at"`
	Result                json.RawMessage `json:"result"`
	Error                 *RuntimeError   `json:"error"`
}

type Trap struct {
	ID                     string
	OrganizationID         string
	ProfileID              string
	TypeID                 string
	TypeVersion            int32
	AppliedProfileRevision *int32
	ActiveCommandID        *string
}

type Prepared struct {
	Action                string
	Params                json.RawMessage
	TargetProfileRevision *int32
	Snapshot              *profiles.Snapshot
}

type CreateResult struct {
	Command  Command
	Replayed bool
}

type Dispatch struct {
	CommandID      string             `json:"command_id"`
	LeaseID        string             `json:"lease_id"`
	LeaseExpiresAt time.Time          `json:"lease_expires_at"`
	Action         string             `json:"action"`
	Params         json.RawMessage    `json:"params"`
	Configuration  *profiles.Snapshot `json:"configuration"`
	ExpiresAt      time.Time          `json:"expires_at"`
}

type AgentRuntime struct {
	RuntimeState           string        `json:"runtime_state"`
	AppliedProfileRevision *int32        `json:"applied_profile_revision"`
	BufferedEvents         int64         `json:"buffered_events"`
	BufferBytes            int64         `json:"buffer_bytes"`
	BufferCapacityBytes    int64         `json:"buffer_capacity_bytes"`
	BufferState            string        `json:"buffer_state"`
	LastError              *RuntimeError `json:"last_error"`
}

type AgentResult struct {
	CommandID string          `json:"command_id"`
	LeaseID   string          `json:"lease_id"`
	Status    Status          `json:"status"`
	Result    json.RawMessage `json:"result"`
	Error     *RuntimeError   `json:"error"`
	Runtime   AgentRuntime    `json:"runtime"`
}

type ListQuery struct {
	Limit     int
	Status    Status
	AfterTime time.Time
	AfterID   string
}

type Page struct {
	Items      []Command `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}

// SnapshotReader is invoked while the trap and current profile are transactionally pinned.
type SnapshotReader interface {
	Current(context.Context, string, string, int32) (profiles.Snapshot, error)
}
