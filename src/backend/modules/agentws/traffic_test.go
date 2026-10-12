package agentws

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/traffic"
)

type trafficGateway struct{ fakeGateway }

func (f *trafficGateway) Authenticate(ctx context.Context, token string) (Identity, error) {
	identity, err := f.fakeGateway.Authenticate(ctx, "secret")
	if token == "target" {
		identity.TrapID = "33333333-3333-4333-8333-333333333333"
	} else if token != "source" {
		return Identity{}, contract.NewError("agent_unauthenticated")
	}

	return identity, err
}

func TestTrafficWebSocketRelay(t *testing.T) {
	gateway := &trafficGateway{fakeGateway: fakeGateway{token: "secret"}}
	h := NewHandler(gateway, &fakeCommands{})
	targetID := "33333333-3333-4333-8333-333333333333"
	h.TrafficTargets = func(ctx context.Context, org, source string) ([]string, error) {
		if err := contract.RequireAgentTrap(ctx, contract.ID(source)); err != nil {
			return nil, err
		}

		if org != testOrgID || source != testTrapID {
			t.Errorf("unexpected source: %s/%s", org, source)
		}

		return []string{targetID}, nil
	}
	router := gin.New()
	router.Use(contract.Middleware())
	h.Register(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()

	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/assets/stream"
	target, response, err := dialAgent(t, endpoint, "target")
	if response != nil {
		response.Body.Close()
	}

	if err != nil {
		t.Fatal(err)
	}

	defer target.Close()

	hello := helloPayload()
	hello["traffic_mirroring"] = true
	sendAgent(t, target, "agent.hello", hello)
	readAgent(t, target, "agent.welcome")
	source, response, err := dialAgent(t, endpoint, "source")
	if response != nil {
		response.Body.Close()
	}

	if err != nil {
		t.Fatal(err)
	}

	defer source.Close()

	sendAgent(t, source, "agent.hello", hello)
	readAgent(t, source, "agent.welcome")
	input := traffic.Frame{
		ConnectionID:  string(contract.NewID()),
		Sequence:      1,
		Kind:          "data",
		RemoteAddress: "127.0.0.1:1234",
		LocalAddress:  "127.0.0.1:22",
		Data:          []byte{0, 255, 1},
	}
	for n, kind := range []string{"open", "data", "close"} {
		frame := input
		frame.Sequence, frame.Kind = int64(n+1), kind
		if kind != "data" {
			frame.Data = nil
		}

		sendAgent(t, source, "traffic.frame", frame)
		readAgent(t, source, "traffic.ack")
		message := readAgent(t, target, "traffic.received")
		var delivery traffic.Delivery
		if err := json.Unmarshal(message["payload"], &delivery); err != nil {
			t.Fatal(err)
		}

		if delivery.SourceID != testTrapID || delivery.TargetID != targetID || string(delivery.Data) != string(frame.Data) || delivery.Kind != kind || delivery.Sequence != frame.Sequence {
			t.Fatal(delivery)
		}
	}

	// Mirroring has no effect on telemetry ingestion and cannot choose arbitrary destinations.
	spoofed := map[string]any{
		"connection_id":  input.ConnectionID,
		"sequence":       2,
		"kind":           "data",
		"remote_address": "remote",
		"local_address":  "local",
		"data":           input.Data,
		"target_id":      targetID,
	}
	sendAgent(t, source, "traffic.frame", spoofed)
	readAgent(t, source, "error")
	gateway.mu.Lock()
	defer gateway.mu.Unlock()

	if len(gateway.batches) != 0 {
		t.Fatal("traffic entered telemetry")
	}
}

func TestTrafficRelayIsDirectBoundedAndOrganizationScoped(t *testing.T) {
	gateway := &fakeGateway{token: "secret"}
	h := NewHandler(gateway, &fakeCommands{})
	source := &agentSession{identity: Identity{OrganizationID: testOrgID, TrapID: testTrapID}, token: "secret", ctx: t.Context()}
	targetID := "33333333-3333-4333-8333-333333333333"
	foreignID := "44444444-4444-4444-8444-444444444444"
	h.TrafficTargets = func(context.Context, string, string) ([]string, error) {
		return []string{targetID, foreignID}, nil
	}
	target := &agentSession{
		identity:       Identity{OrganizationID: testOrgID, TrapID: targetID},
		trafficEnabled: true,
		traffic:        make(chan traffic.Delivery, 1),
	}
	target.active.Store(true)
	foreign := &agentSession{
		identity:       Identity{OrganizationID: "other", TrapID: foreignID},
		trafficEnabled: true,
		traffic:        make(chan traffic.Delivery, 1),
	}
	foreign.active.Store(true)
	h.registry.entry(targetID).current = target
	h.registry.entry(foreignID).current = foreign
	frame := traffic.Frame{
		ConnectionID:  string(contract.NewID()),
		Sequence:      1,
		Kind:          "data",
		RemoteAddress: "127.0.0.1:1234",
		LocalAddress:  "127.0.0.1:22",
		Data:          []byte{0, 255},
	}
	sent, skipped, err := h.routeTraffic(source, frame)
	if err != nil || sent != 1 || skipped != 1 {
		t.Fatalf("sent=%d skipped=%d err=%v", sent, skipped, err)
	}

	sent, skipped, err = h.routeTraffic(source, frame)
	if err != nil || sent != 0 || skipped != 2 {
		t.Fatalf("full queue: %d/%d %v", sent, skipped, err)
	}

	got := <-target.traffic
	if got.SourceID != testTrapID || got.TargetID != targetID || got.Data[1] != 255 {
		t.Fatal(got)
	}

	if len(foreign.traffic) != 0 {
		t.Fatal("organization leak")
	}

	target.active.Store(false)
	sent, _, err = h.routeTraffic(source, frame)
	if err != nil || sent != 0 {
		t.Fatalf("inactive session received copy: %d %v", sent, err)
	}
}

func TestTrafficValidation(t *testing.T) {
	for _, tt := range []struct {
		name  string
		kind  string
		data  []byte
		valid bool
	}{
		{"binary", "data", []byte{0, 255}, true}, {"empty data", "data", nil, false},
		{"empty decoded data", "data", []byte{}, false},
		{"open", "open", nil, true}, {"close", "close", nil, true},
		{"open with empty bytes", "open", []byte{}, false}, {"maximum chunk", "data", make([]byte, traffic.MaxChunkBytes), true},
		{"open with bytes", "open", []byte{1}, false}, {"oversize", "data", make([]byte, traffic.MaxChunkBytes+1), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			frame := traffic.Frame{
				ConnectionID:  string(contract.NewID()),
				Sequence:      1,
				Kind:          tt.kind,
				RemoteAddress: "remote",
				LocalAddress:  "local",
				Data:          tt.data,
			}
			if err := traffic.CheckFrame(frame); (err == nil) != tt.valid {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
