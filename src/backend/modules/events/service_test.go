package events

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/catalog"
)

type memoryIngest struct {
	calls  int
	events []AgentEvent
}

func (f *memoryIngest) Ingest(_ context.Context, _ agentws.Identity, _, id string, events []AgentEvent) (agentws.TelemetryAck, error) {
	f.calls++
	f.events = events
	return acknowledge(id, events, time.Now()), nil
}
func TestIngestBoundary(t *testing.T) {
	codec, err := contract.NewCursorCodec([]byte(strings.Repeat("x", 32)))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	identity := agentws.Identity{TrapID: string(contract.NewID()), OrganizationID: string(contract.NewID()), TypeID: "tcp-banner", TypeVersion: 1}
	event := AgentEvent{EventID: string(contract.NewID()), EventType: "tcp.connection_opened", TypeID: "tcp-banner", TypeVersion: 1, ProfileRevision: 1, OccurredAt: "2000-01-01T00:00:00Z", SessionID: string(contract.NewID()), SessionSequence: 1, Source: Source{IP: "192.0.2.1", Port: 50000}, Destination: Destination{Protocol: "tcp", Port: 2222}, Data: json.RawMessage(`{"listener_name":"ssh"}`)}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, code           string
		role                 contract.Role
		org                  string
		duplicate, uppercase bool
	}{
		{name: "success", role: contract.Agent, org: identity.OrganizationID},
		{name: "uppercase UUID", role: contract.Agent, org: identity.OrganizationID, uppercase: true},
		{name: "operator", role: contract.Admin, org: identity.OrganizationID, code: "forbidden"},
		{name: "foreign organization", role: contract.Agent, org: string(contract.NewID()), code: "resource_not_found"},
		{name: "duplicate", role: contract.Agent, org: identity.OrganizationID, duplicate: true, code: "validation_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memoryIngest{}
			s := NewService(repo, cat)
			ctx := contract.WithPrincipal(t.Context(), contract.Principal{Role: tc.role, OrganizationID: contract.ID(tc.org), TrapID: contract.ID(identity.TrapID)})
			batch := agentws.TelemetryBatch{BatchID: string(contract.NewID()), Events: []agentws.Event{{EventID: event.EventID, Raw: raw}}}
			if tc.uppercase {
				batch.Events[0].EventID = strings.ToUpper(event.EventID)
				batch.Events[0].Raw = json.RawMessage(strings.Replace(string(raw), event.EventID, batch.Events[0].EventID, 1))
			}
			if tc.duplicate {
				batch.Events = append(batch.Events, batch.Events[0])
			}
			_, err := s.Ingest(ctx, identity, "connection", batch)
			if tc.code == "" {
				if err != nil || repo.calls != 1 {
					t.Fatalf("calls=%d err=%v", repo.calls, err)
				}
				return
			}
			var api *contract.Error
			if !errors.As(err, &api) || api.Code != tc.code || repo.calls != 0 {
				t.Fatalf("calls=%d err=%v, want %s", repo.calls, err, tc.code)
			}
		})
	}
}
