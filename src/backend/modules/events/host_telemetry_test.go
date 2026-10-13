package events

import (
	"encoding/json"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

func hostEvent(t *testing.T) AgentEvent {
	t.Helper()
	e := AgentEvent{EventID: string(contract.NewID()), EventType: "process.exec", TypeID: "ssh-high-interaction", TypeVersion: 2, ProfileRevision: 1,
		OccurredAt: "2000-01-01T12:00:01Z", SessionID: string(contract.NewID()), SessionSequence: 1,
		Source: Source{IP: "192.0.2.1", Port: 50000}, Destination: Destination{Protocol: "tcp", Port: 2222}}
	e.Data = json.RawMessage(`{"service":"ssh","schema_version":1,"sensor":"tetragon","sensor_version":"1.7.0","host_id":"node-a","generation":1,"container_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","container_started_at":"2000-01-01T12:00:00Z","pid":2345,"ppid":1,"uid":1000,"binary":"/usr/bin/id","cwd":"/home/sandbox","arguments_redacted":true}`)
	return e
}

func TestHostTelemetryIngestAuthorization(t *testing.T) {
	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		t.Fatal(err)
	}
	identity := agentws.Identity{TrapID: string(contract.NewID()), OrganizationID: string(contract.NewID()), TypeID: "ssh-high-interaction", TypeVersion: 2, CredentialGeneration: 1}
	for _, tc := range []struct {
		name            string
		role            contract.Role
		foreign, future bool
		previous        bool
		ok              bool
	}{
		{name: "agent", role: contract.Agent, ok: true},
		{name: "earlier generation replay", role: contract.Agent, previous: true, ok: true},
		{name: "operator cannot ingest", role: contract.Admin},
		{name: "foreign trap", role: contract.Agent, foreign: true},
		{name: "future generation", role: contract.Agent, future: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			currentIdentity := identity
			if tc.previous {
				currentIdentity.CredentialGeneration = 2
			}
			e := hostEvent(t)
			if tc.future {
				var data map[string]any
				if err := json.Unmarshal(e.Data, &data); err != nil {
					t.Fatal(err)
				}
				data["generation"] = 2
				e.Data, err = json.Marshal(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			trap := identity.TrapID
			if tc.foreign {
				trap = string(contract.NewID())
			}
			ctx := contract.WithPrincipal(t.Context(), contract.Principal{Role: tc.role, OrganizationID: contract.ID(identity.OrganizationID), TrapID: contract.ID(trap)})
			repo := &memoryIngest{}
			_, err = NewService(repo, cat).Ingest(ctx, currentIdentity, "connection", agentws.TelemetryBatch{BatchID: string(contract.NewID()), Events: []agentws.Event{{EventID: e.EventID, Raw: raw}}})
			if (err == nil) != tc.ok || tc.ok && repo.calls != 1 || !tc.ok && repo.calls != 0 {
				t.Fatalf("calls=%d err=%v", repo.calls, err)
			}
		})
	}
}

func TestHostTelemetrySnapshotBoundaries(t *testing.T) {
	e := hostEvent(t)
	snapshot := profiles.Snapshot{TypeID: "ssh-high-interaction", TypeVersion: 2, ProfileRevision: 1, Config: profiles.Object{"services": []any{profiles.Object{"name": "ssh", "port": 2222}}}}
	for _, tc := range []struct {
		name   string
		mutate func(*AgentEvent)
		ok     bool
	}{
		{name: "matching service", mutate: func(*AgentEvent) {}, ok: true},
		{name: "wrong service port", mutate: func(e *AgentEvent) { e.Destination.Port = 3333 }},
		{name: "old event", mutate: func(e *AgentEvent) {
			e.OccurredAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
		}},
		{name: "wrong profile", mutate: func(e *AgentEvent) { e.ProfileRevision = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := e
			tc.mutate(&changed)
			if err := checkSnapshot(changed, snapshot); (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
