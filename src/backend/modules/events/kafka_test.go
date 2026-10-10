package events

import (
	"context"
	"errors"
	"testing"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
)

type fakePublisher struct {
	err   error
	calls int
}

type unavailableStore struct{}

func (unavailableStore) Ingest(context.Context, agentws.Identity, string, string, []AgentEvent) (agentws.TelemetryAck, error) {
	return agentws.TelemetryAck{}, contract.NewError("database_unavailable")
}

func TestBrokerAckIsNotTelemetryAck(t *testing.T) {
	publisher := &fakePublisher{}
	s := journalStore{unavailableStore{}, publisher}
	ack, err := s.Ingest(t.Context(), agentws.Identity{}, "connection", "batch", []AgentEvent{{EventID: "event"}})
	var api *contract.Error
	if !errors.As(err, &api) || api.Code != "database_unavailable" || ack.BatchID != "" || publisher.calls != 1 {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
}

func (f *fakePublisher) Publish(_ context.Context, _ agentws.Identity, _ string, _ []AgentEvent) error {
	f.calls++
	return f.err
}

func TestJournalBeforeMaterialization(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		calls int
	}{
		{"broker success", nil, 1}, {"broker failure", errors.New("offline"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := &memoryIngest{}
			publisher := &fakePublisher{err: tc.err}
			store := journalStore{db, publisher}
			_, err := store.Ingest(t.Context(), agentws.Identity{}, "connection", "batch", []AgentEvent{{EventID: "event"}})
			if db.calls != tc.calls || publisher.calls != 1 || (err == nil) != (tc.err == nil) {
				t.Fatalf("db=%d broker=%d err=%v", db.calls, publisher.calls, err)
			}
			if tc.err != nil {
				var api *contract.Error
				if !errors.As(err, &api) || api.Code != "telemetry_unavailable" {
					t.Fatalf("err=%v", err)
				}
			}
		})
	}
}

func (unavailableStore) Prepare(context.Context, agentws.Identity, string, string, []AgentEvent) error {
	return nil
}
func (*memoryIngest) Prepare(context.Context, agentws.Identity, string, string, []AgentEvent) error {
	return nil
}
