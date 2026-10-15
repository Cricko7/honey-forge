package agentws

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/traffic"
	"honey-forge/modules/commands"
)

const sentinelTrapID = "44444444-4444-4444-8444-444444444444"

type queuedTrafficGateway struct{ trafficGateway }

func (g *queuedTrafficGateway) Authenticate(ctx context.Context, token string) (Identity, error) {
	if token == "sentinel" {
		identity, err := g.trafficGateway.Authenticate(ctx, "source")
		identity.TrapID = sentinelTrapID
		return identity, err
	}
	return g.trafficGateway.Authenticate(ctx, token)
}

type queuedTrafficCommands struct {
	fakeCommands
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *queuedTrafficCommands) Claim(ctx context.Context, org, trap string) (*commands.Dispatch, error) {
	if trap == "33333333-3333-4333-8333-333333333333" {
		f.once.Do(func() {
			close(f.started)
			select {
			case <-f.release:
			case <-ctx.Done():
			}
		})
	}
	return nil, nil
}

func TestQueuedTrafficRechecksSourceAndLink(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Handler, *agentSession, *bool)
		want   string
	}{
		{"authorized", func(*Handler, *agentSession, *bool) {}, "original"},
		{"link deleted", func(_ *Handler, _ *agentSession, linked *bool) { *linked = false }, "sentinel"},
		{"source revoked", func(_ *Handler, source *agentSession, _ *bool) { source.active.Store(false) }, "sentinel"},
		{"source replaced", func(h *Handler, source *agentSession, _ *bool) {
			entry := h.registry.lookup(source.identity.TrapID)
			entry.mu.Lock()
			entry.current = &agentSession{}
			entry.mu.Unlock()
		}, "sentinel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := &queuedTrafficGateway{trafficGateway{fakeGateway{token: "secret"}}}
			commandService := &queuedTrafficCommands{started: make(chan struct{}), release: make(chan struct{})}
			h := NewHandler(gateway, commandService)
			const targetID = "33333333-3333-4333-8333-333333333333"
			linked := true
			h.TrafficTargets = func(ctx context.Context, org, source string) ([]string, error) {
				if err := contract.RequireAgentTrap(ctx, contract.ID(source)); err != nil {
					return nil, err
				}
				if source == sentinelTrapID || linked {
					return []string{targetID}, nil
				}
				return nil, nil
			}
			router := gin.New()
			router.Use(contract.Middleware())
			h.Register(router)
			server := httptest.NewTLSServer(router)
			defer server.Close()
			var release sync.Once
			defer release.Do(func() { close(commandService.release) })
			target, response, err := dialAgent(t, "wss"+strings.TrimPrefix(server.URL, "https")+"/assets/stream", "target")
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
			select {
			case <-commandService.started:
			case <-time.After(3 * time.Second):
				t.Fatal("target did not reach claim barrier")
			}
			// Claim holds the target entry lock, so its current session cannot change
			// until release. Queue both frames before allowing the consumer to run.
			targetSession := h.registry.lookup(targetID).current
			for _, token := range []string{"source", "sentinel"} {
				identity, err := gateway.Authenticate(t.Context(), token)
				if err != nil {
					t.Fatal(err)
				}
				ctx := contract.WithPrincipal(t.Context(), contract.Principal{OrganizationID: contract.ID(testOrgID), TrapID: contract.ID(identity.TrapID), Role: contract.Agent})
				source := &agentSession{identity: identity, token: token, ctx: ctx}
				source.active.Store(true)
				h.registry.entry(identity.TrapID).current = source
				data := token
				if token == "source" {
					data = "original"
				}
				targetSession.traffic <- trafficDelivery{
					delivery: traffic.Delivery{SourceID: identity.TrapID, TargetID: targetID, Frame: traffic.Frame{
						ConnectionID: string(contract.NewID()), Sequence: 1, Kind: "data",
						RemoteAddress: "127.0.0.1:1234", LocalAddress: "127.0.0.1:22", Data: []byte(data),
					}},
					source: source,
				}
				if token == "source" {
					tc.change(h, source, &linked)
				}
			}
			release.Do(func() { close(commandService.release) })
			message := readAgent(t, target, "traffic.received")
			var delivery traffic.Delivery
			if err := json.Unmarshal(message["payload"], &delivery); err != nil {
				t.Fatal(err)
			}
			if string(delivery.Data) != tc.want {
				t.Fatalf("received %q after %s; want %q", delivery.Data, tc.name, tc.want)
			}
		})
	}
}

func TestTrafficOppositeLinksAvoidDeadlock(t *testing.T) {
	gateway := &trafficGateway{fakeGateway{token: "secret"}}
	h := NewHandler(gateway, &fakeCommands{})
	const targetID = "33333333-3333-4333-8333-333333333333"
	h.TrafficTargets = func(_ context.Context, _, source string) ([]string, error) {
		if source == testTrapID {
			return []string{targetID}, nil
		}
		return []string{testTrapID}, nil
	}
	router := gin.New()
	router.Use(contract.Middleware())
	h.Register(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()
	var clients []*websocket.Conn
	for _, token := range []string{"source", "target"} {
		client, response, err := dialAgent(t, "wss"+strings.TrimPrefix(server.URL, "https")+"/assets/stream", token)
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		hello := helloPayload()
		hello["traffic_mirroring"] = true
		sendAgent(t, client, "agent.hello", hello)
		readAgent(t, client, "agent.welcome")
		clients = append(clients, client)
	}
	for range 8 {
		for _, client := range clients {
			sendAgent(t, client, "traffic.frame", traffic.Frame{
				ConnectionID: string(contract.NewID()), Sequence: 1, Kind: "data",
				RemoteAddress: "127.0.0.1:1234", LocalAddress: "127.0.0.1:22", Data: []byte("copy"),
			})
		}
		for _, client := range clients {
			if err := client.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			seen := map[string]int{}
			for range 2 {
				var message contract.Envelope
				if err := client.ReadJSON(&message); err != nil {
					t.Fatal(err)
				}
				seen[message.Type]++
			}
			if seen["traffic.ack"] != 1 || seen["traffic.received"] != 1 {
				t.Fatalf("opposite relay did not complete: %v", seen)
			}
		}
	}
}
