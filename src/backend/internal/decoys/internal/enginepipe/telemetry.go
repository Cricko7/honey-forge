package enginepipe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"
	"unicode/utf8"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

// Only protocol observations cross this pipe. Identity, revision, transport and
// listener attribution always come from the agent's pinned configuration.
type message struct {
	Username      string  `json:"username,omitempty"`
	Password      *string `json:"password,omitempty"`
	Kind          string  `json:"kind"`
	SessionID     string  `json:"session_id,omitempty"`
	SourceIP      string  `json:"source_ip,omitempty"`
	SourcePort    int     `json:"source_port,omitempty"`
	Input         string  `json:"input,omitempty"`
	Truncated     bool    `json:"truncated,omitempty"`
	ReceivedBytes int64   `json:"received_bytes,omitempty"`
	DurationMS    int64   `json:"duration_ms,omitempty"`
	BytesReceived int64   `json:"bytes_received,omitempty"`
	Reason        string  `json:"reason,omitempty"`
}

type session struct {
	source   events.Source
	sequence int64
	bytes    int64
}

type bridge struct {
	snapshot profiles.Snapshot
	config   Config
	sessions map[string]*session // owned by the pipe reader
}

func (b *bridge) accept(ctx context.Context, m message, sink func(context.Context, events.AgentEvent) error) error {
	if !contract.ValidID(m.SessionID) {
		return fmt.Errorf("invalid engine session identity")
	}
	s := b.sessions[m.SessionID]
	service := b.config.Services[0]
	data := map[string]any{"service": service.Name}
	kind := ""
	switch m.Kind {
	case "opened":
		ip, err := netip.ParseAddr(m.SourceIP)
		if err != nil || ip.Zone() != "" || m.SourcePort < 1 || m.SourcePort > 65535 || s != nil || len(b.sessions) >= b.config.Session.MaxConnections {
			return fmt.Errorf("invalid engine session opening")
		}
		s = &session{source: events.Source{IP: ip.Unmap().String(), Port: m.SourcePort}}
		kind = "service.connection_opened"
	case "action":
		if s == nil || !utf8.ValidString(m.Input) || len(m.Input) > 1024 || m.ReceivedBytes < 0 || m.ReceivedBytes > 8192 || (s != nil && s.bytes+m.ReceivedBytes > 1<<20) {
			return fmt.Errorf("invalid engine action")
		}
		kind = "service.action"
		data["action_kind"], data["input"], data["outcome"] = "request", m.Input, "unknown"
		data["truncated"], data["received_bytes"] = m.Truncated, m.ReceivedBytes
	case "auth":
		if b.snapshot.TypeID != "dionaea" || s == nil || m.ReceivedBytes != 0 || !utf8.ValidString(m.Username) || (m.Password != nil && (!utf8.ValidString(*m.Password) || len(*m.Password) > 1024)) || len(m.Username) > 1024 {
			return fmt.Errorf("invalid engine authentication")
		}
		kind = "service.auth_attempt"
		data["username"], data["password"], data["outcome"] = m.Username, m.Password, "unknown"
		data["truncated"], data["received_bytes"] = m.Truncated, int64(0)
	case "closed":
		if s == nil || m.DurationMS < 0 || m.BytesReceived < s.bytes || m.BytesReceived > 1<<20 {
			return fmt.Errorf("invalid engine session closing")
		}
		switch m.Reason {
		case "peer_closed", "idle_timeout", "service_stopped", "network_error", "quit":
		default:
			return fmt.Errorf("invalid engine close reason")
		}
		kind = "service.connection_closed"
		data["duration_ms"], data["bytes_received"], data["reason"] = m.DurationMS, m.BytesReceived, m.Reason
	default:
		return fmt.Errorf("unknown engine observation")
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode engine event: %w", err)
	}
	e := events.AgentEvent{
		EventID: string(contract.NewID()), EventType: kind, TypeID: b.snapshot.TypeID, TypeVersion: int64(b.snapshot.TypeVersion),
		ProfileRevision: int64(b.snapshot.ProfileRevision), OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: m.SessionID, SessionSequence: s.sequence + 1, Source: s.source,
		Destination: events.Destination{Protocol: transport(service.Protocol), Port: service.Port}, Data: raw,
	}
	eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := sink(eventCtx, e); err != nil {
		return fmt.Errorf("deliver engine event: %w", err)
	}
	s.sequence++
	s.bytes += m.ReceivedBytes
	if m.Kind == "closed" {
		delete(b.sessions, m.SessionID)
	} else {
		b.sessions[m.SessionID] = s
	}
	return nil
}

func transport(protocol string) string {
	if protocol == "snmp" || protocol == "bacnet" {
		return "udp"
	}
	return "tcp"
}
