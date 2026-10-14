package agentws

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"honey-forge/internal/contract"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"honey-forge/internal/platform/pipeline"
	"honey-forge/modules/profiles"
)

func TestPipelineRoutesAreAuthorizedBoundedAndContinueAcrossTraps(t *testing.T) {
	const targetID = "33333333-3333-4333-8333-333333333333"
	for _, tc := range []struct {
		name                     string
		linked, foreign, offline bool
		valid                    bool
	}{
		{"connected target", true, false, false, true},
		{"no link", false, false, false, false},
		{"other organization", true, true, false, false},
		{"offline", true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(&fakeGateway{}, &fakeCommands{})
			h.TrafficTargets = func(context.Context, string, string) ([]string, error) {
				if tc.linked {
					return []string{targetID}, nil
				}
				return nil, nil
			}
			h.PipelineSnapshot = func(context.Context, Identity, string) (profiles.Snapshot, error) { return pipelineTestSnapshot(), nil }
			source := &agentSession{identity: Identity{OrganizationID: testOrgID, TrapID: testTrapID, TypeID: "custom-http"}, ctx: t.Context()}
			target := &agentSession{identity: Identity{OrganizationID: testOrgID, TrapID: targetID, TypeID: "custom-http"}, pipelineEnabled: true, pipeline: make(chan pipelineDelivery, 1)}
			if tc.foreign {
				target.identity.OrganizationID = "other"
			}
			target.active.Store(!tc.offline)
			h.registry.entry(targetID).current = target
			r := pipeline.Request{NodeID: "send", TargetID: targetID, Service: "web", Method: "POST", Path: "/pipeline", Payload: json.RawMessage(`{"count":3}`), SourceIP: "127.0.0.1", SourcePort: 1234}
			if err := h.routePipeline(source, r); (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if !tc.valid {
				return
			}
			d := (<-target.pipeline).delivery
			if d.TraceID == "" || d.Token == "" || d.Hop != 1 || string(d.Payload) != string(r.Payload) {
				t.Fatalf("delivery: %+v", d)
			}
			// A second forward shares its trace and total hop budget; tokens belong to the receiver.
			r.Token = d.Token
			if err := h.routePipeline(source, r); err == nil {
				t.Fatal("sender reused receiver token")
			}
			source.identity.TrapID = targetID
			if err := h.routePipeline(source, r); err != nil {
				t.Fatal(err)
			}
			next := (<-target.pipeline).delivery
			if next.TraceID != d.TraceID || next.Hop != 2 {
				t.Fatal(next)
			}
			if err := h.routePipeline(source, r); err != nil {
				t.Fatal(err)
			}
			if err := h.routePipeline(source, r); err == nil {
				t.Fatal("unbounded queue")
			}
		})
	}
}

func pipelineTestSnapshot() profiles.Snapshot {
	return profiles.Snapshot{TypeID: "custom-http", Config: profiles.Object{
		"services": []any{profiles.Object{"name": "web", "entry": "web"}},
		"blocks": []any{
			profiles.Object{"id": "web", "kind": "input", "config": profiles.Object{"method": "POST", "path": "/pipeline"}},
			profiles.Object{"id": "send", "kind": "send", "config": profiles.Object{"target_id": "33333333-3333-4333-8333-333333333333", "service": "web", "method": "POST", "path": "/pipeline"}},
		},
	}}
}

type pipelineGateway struct{ trafficGateway }

func (g *pipelineGateway) Authenticate(ctx context.Context, token string) (Identity, error) {
	i, err := g.trafficGateway.Authenticate(ctx, token)
	i.TypeID = "custom-http"
	return i, err
}

func TestPipelineWebSocketAdmissionAndRevokedLink(t *testing.T) {
	gateway := &pipelineGateway{trafficGateway{fakeGateway: fakeGateway{token: "secret"}}}
	h := NewHandler(gateway, &fakeCommands{})
	const targetID = "33333333-3333-4333-8333-333333333333"
	var mu sync.Mutex
	linked := true
	h.TrafficTargets = func(context.Context, string, string) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		if linked {
			return []string{targetID}, nil
		}
		return nil, nil
	}
	h.PipelineSnapshot = func(context.Context, Identity, string) (profiles.Snapshot, error) { return pipelineTestSnapshot(), nil }
	router := gin.New()
	router.Use(contract.Middleware())
	h.Register(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()
	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream"
	target, res, err := dialAgent(t, endpoint, "target")
	if res != nil {
		res.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	hello := helloPayload()
	hello["http_pipeline"] = true
	hello["supported_types"] = []SupportedType{{TypeID: "custom-http", TypeVersion: 1, Actions: []string{"start", "stop", "apply_config"}}}
	sendAgent(t, target, "agent.hello", hello)
	readAgent(t, target, "agent.welcome")
	source, res, err := dialAgent(t, endpoint, "source")
	if res != nil {
		res.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	sendAgent(t, source, "agent.hello", hello)
	readAgent(t, source, "agent.welcome")
	r := pipeline.Request{RequestID: string(contract.NewID()), NodeID: "send", TargetID: targetID, Service: "web", Method: "POST", Path: "/pipeline", Payload: json.RawMessage(`{"count":3}`), SourceIP: "127.0.0.1", SourcePort: 1234}
	sendAgent(t, source, "pipeline.send", r)
	ack := readAgent(t, source, "pipeline.ack")
	if !strings.Contains(string(ack["payload"]), `"accepted":true`) {
		t.Fatal(ack)
	}
	message := readAgent(t, target, "pipeline.received")
	var payload struct {
		Delivery pipeline.Delivery `json:"delivery"`
	}
	if err := json.Unmarshal(message["payload"], &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Delivery.SourceID != testTrapID || string(payload.Delivery.Payload) != string(r.Payload) {
		t.Fatal(payload)
	}
	mu.Lock()
	linked = false
	mu.Unlock()
	r.RequestID = string(contract.NewID())
	sendAgent(t, source, "pipeline.send", r)
	ack = readAgent(t, source, "pipeline.ack")
	if !strings.Contains(string(ack["payload"]), `"accepted":false`) {
		t.Fatal(ack)
	}
	sendAgent(t, source, "agent.heartbeat", map[string]any{"runtime": hello["runtime"]})
	readAgent(t, source, "agent.heartbeat_ack")
}

func TestPipelineContinuationExpiryAndSharedConcurrentBudget(t *testing.T) {
	h := NewHandler(&fakeGateway{}, &fakeCommands{})
	s := &agentSession{identity: Identity{OrganizationID: testOrgID, TrapID: testTrapID}}
	r := pipeline.Request{TargetID: testTrapID, SourceIP: "127.0.0.1", SourcePort: 1234}
	now := time.Now()
	d, err := h.pipelineHandoff(s, s, r, now)
	if err != nil {
		t.Fatal(err)
	}
	r.Token = d.Token
	if _, err := h.pipelineHandoff(s, s, r, now.Add(pipeline.Lifetime)); err == nil {
		t.Fatal("expired continuation accepted")
	}
	d, err = h.pipelineHandoff(s, s, pipeline.Request{TargetID: testTrapID}, now)
	if err != nil {
		t.Fatal(err)
	}
	r.Token = d.Token
	s.connectionID = "replacement"
	if _, err := h.pipelineHandoff(s, s, r, now); err == nil {
		t.Fatal("replaced connection reused continuation")
	}
	s.connectionID = ""
	s.identity.CredentialGeneration++
	if _, err := h.pipelineHandoff(s, s, r, now); err == nil {
		t.Fatal("rotated credential reused continuation")
	}
	s.identity.CredentialGeneration--
	h.pipelineTokens[d.Token].trace.hops = pipeline.MaxHops - 1
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, err := h.pipelineHandoff(s, s, r, now); results <- err })
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("budget race: accepted %d", accepted)
	}
}
