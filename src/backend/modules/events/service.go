package events

import (
	"context"
	"strings"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/catalog"
)

type eventStore interface {
	Ingest(context.Context, agentws.Identity, string, string, []AgentEvent) (agentws.TelemetryAck, error)
}
type Service struct {
	repository eventStore
	catalog    *catalog.Service
}

func NewService(repository eventStore, cat *catalog.Service) *Service {
	return &Service{repository, cat}
}
func (s *Service) Ingest(ctx context.Context, identity agentws.Identity, connection string, batch agentws.TelemetryBatch) (agentws.TelemetryAck, error) {
	if err := contract.RequireAgentTrap(ctx, contract.ID(identity.TrapID)); err != nil {
		return agentws.TelemetryAck{}, err
	}
	if err := contract.RequireOrganization(ctx, contract.ID(identity.OrganizationID)); err != nil {
		return agentws.TelemetryAck{}, err
	}
	if !contract.ValidID(batch.BatchID) || len(batch.Events) == 0 || len(batch.Events) > 100 {
		return agentws.TelemetryAck{}, contract.NewError("validation_failed")
	}
	now := time.Now().UTC()
	events := make([]AgentEvent, 0, len(batch.Events))
	seen := map[string]bool{}
	for _, raw := range batch.Events {
		event, err := parseEvent(raw.Raw, now)
		if err != nil {
			return agentws.TelemetryAck{}, err
		}
		if !strings.EqualFold(event.EventID, raw.EventID) || seen[event.EventID] {
			return agentws.TelemetryAck{}, contract.NewError("validation_failed")
		}
		seen[event.EventID] = true
		if event.TypeID != identity.TypeID || event.TypeVersion != int64(identity.TypeVersion) {
			return agentws.TelemetryAck{}, contract.NewError("telemetry_invalid")
		}
		if err := s.catalog.CheckEvent(ctx, event.TypeID, contract.TypeVersion(event.TypeVersion), event.EventType, event.Data); err != nil {
			return agentws.TelemetryAck{}, err
		}
		events = append(events, event)
	}
	return s.repository.Ingest(ctx, identity, connection, batch.BatchID, events)
}
