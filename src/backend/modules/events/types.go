// Package events validates, journals, persists and reads attack telemetry.
package events

import (
	"encoding/json"
	"time"
)

type Source struct {
	IP   string `json:"ip" validate:"required"`
	Port int    `json:"port" validate:"min=1,max=65535"`
}
type Destination struct {
	Protocol string `json:"protocol" validate:"min=1,max=32"`
	Port     int    `json:"port" validate:"min=1,max=65535"`
}
type AgentEvent struct {
	EventID         string          `json:"event_id" validate:"required,uuid"`
	EventType       string          `json:"event_type" validate:"required,type_id"`
	TypeID          string          `json:"type_id" validate:"required,type_id"`
	TypeVersion     int64           `json:"type_version" validate:"min=1,max=2147483647"`
	ProfileRevision int64           `json:"profile_revision" validate:"min=1,max=2147483647"`
	OccurredAt      string          `json:"occurred_at" validate:"required"`
	SessionID       string          `json:"session_id" validate:"required,uuid"`
	SessionSequence int64           `json:"session_sequence" validate:"min=1,max=9007199254740991"`
	Source          Source          `json:"source"`
	Destination     Destination     `json:"destination"`
	Data            json.RawMessage `json:"data,omitempty" validate:"required"`
}
type Enrichment struct {
	CountryCode *string `json:"country_code"`
	ASN         *uint32 `json:"asn"`
}
type Event struct {
	AgentEvent
	TrapID           string      `json:"trap_id"`
	ReceivedAt       time.Time   `json:"received_at"`
	SourceEnrichment *Enrichment `json:"source_enrichment,omitempty"`
}

// EventSummary is safe to place on REST lists and event.created notifications:
// attacker credentials and input are available only through Event details.
type EventSummary struct {
	EventID         string      `json:"event_id"`
	EventType       string      `json:"event_type"`
	TypeID          string      `json:"type_id"`
	TypeVersion     int64       `json:"type_version"`
	ProfileRevision int64       `json:"profile_revision"`
	OccurredAt      string      `json:"occurred_at"`
	SessionID       string      `json:"session_id"`
	SessionSequence int64       `json:"session_sequence"`
	Source          Source      `json:"source"`
	Destination     Destination `json:"destination"`
	TrapID          string      `json:"trap_id"`
	ReceivedAt      time.Time   `json:"received_at"`
}

func (e Event) Summary() EventSummary {
	return EventSummary{e.EventID, e.EventType, e.TypeID, e.TypeVersion, e.ProfileRevision, e.OccurredAt, e.SessionID, e.SessionSequence, e.Source, e.Destination, e.TrapID, e.ReceivedAt}
}

type EventPage struct {
	Items        []EventSummary `json:"items"`
	NextCursor   *string        `json:"next_cursor"`
	StreamCursor string         `json:"stream_cursor"`
}
type Query struct {
	Limit                                  int
	TrapID, EventType, SessionID, SourceIP string
	From, To                               *time.Time
	Boundary                               int64
	AfterTime                              *time.Time
	AfterID                                string
}
