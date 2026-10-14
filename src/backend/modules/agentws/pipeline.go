package agentws

import (
	"context"
	"fmt"
	"slices"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/pipeline"
)

type pipelineDelivery struct {
	delivery pipeline.Delivery
	source   *agentSession
}
type pipelineTrace struct {
	id, org, ip string
	port, hops  int
	expires     time.Time
}
type pipelineCapability struct {
	target     string
	connection string
	generation int64
	trace      *pipelineTrace
}

func (h *Handler) handlePipeline(entry *sessionEntry, session *agentSession, message contract.Envelope) error {
	var r pipeline.Request
	err := decodePayload(message.Payload, &r)
	if err == nil {
		err = pipeline.Check(r)
	}
	if err == nil && (!session.pipelineEnabled || h.PipelineSnapshot == nil) {
		err = fmt.Errorf("pipeline unavailable")
	}
	if err == nil {
		err = h.withActive(entry, session, func(ctx context.Context) error {
			snapshot, err := h.PipelineSnapshot(ctx, session.identity, session.connectionID)
			if err != nil {
				return err
			}
			if !pipeline.AllowsSend(snapshot, r) {
				return fmt.Errorf("unconfigured pipeline send")
			}
			return nil
		})
	}
	if err == nil {
		err = h.routePipeline(session, r)
	}
	return session.socket.Reply(session.ctx, message, "pipeline.ack", map[string]any{"request_id": r.RequestID, "accepted": err == nil})
}

func (h *Handler) routePipeline(source *agentSession, r pipeline.Request) error {
	if h.TrafficTargets == nil || h.PipelineSnapshot == nil || source.identity.TypeID != "custom-http" {
		return fmt.Errorf("pipeline unavailable")
	}
	targets, err := h.TrafficTargets(source.ctx, source.identity.OrganizationID, source.identity.TrapID)
	if err != nil {
		return err
	}
	if !slices.Contains(targets, r.TargetID) {
		return fmt.Errorf("pipeline link missing")
	}
	entry := h.registry.lookup(r.TargetID)
	if entry == nil {
		return fmt.Errorf("pipeline target offline")
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	target := entry.current
	if target == nil || !target.active.Load() || !target.pipelineEnabled || target.identity.OrganizationID != source.identity.OrganizationID || target.identity.TypeID != "custom-http" {
		return fmt.Errorf("pipeline target unavailable")
	}
	// The source lock is released before visiting another agent, avoiding opposite-link deadlocks.
	if target.token != "" {
		identity, err := h.gateway.Authenticate(target.ctx, target.token)
		if err != nil || !sameIdentity(identity, target.identity) {
			return fmt.Errorf("pipeline target revoked")
		}
	}
	snapshot, err := h.PipelineSnapshot(target.ctx, target.identity, target.connectionID)
	if err != nil {
		return err
	}
	if !pipeline.AllowsReceive(snapshot, r) {
		return fmt.Errorf("pipeline endpoint unavailable")
	}
	d, err := h.pipelineHandoff(source, target, r, time.Now())
	if err != nil {
		return err
	}
	select {
	case target.pipeline <- pipelineDelivery{d, source}:
		return nil
	default:
		return fmt.Errorf("pipeline queue full")
	}
}

func (h *Handler) pipelineHandoff(source, target *agentSession, r pipeline.Request, now time.Time) (pipeline.Delivery, error) {
	h.pipelineMu.Lock()
	defer h.pipelineMu.Unlock()
	if h.pipelineTokens == nil {
		h.pipelineTokens = map[string]pipelineCapability{}
	}
	for key, c := range h.pipelineTokens {
		if !now.Before(c.trace.expires) {
			delete(h.pipelineTokens, key)
		}
	}
	if len(h.pipelineTokens) >= 4096 {
		return pipeline.Delivery{}, fmt.Errorf("pipeline capacity reached")
	}
	trace := &pipelineTrace{id: string(contract.NewID()), org: source.identity.OrganizationID, ip: r.SourceIP, port: r.SourcePort, expires: now.Add(pipeline.Lifetime)}
	if r.Token != "" {
		c, ok := h.pipelineTokens[r.Token]
		if !ok || c.target != source.identity.TrapID || c.trace.org != source.identity.OrganizationID || c.generation != source.identity.CredentialGeneration || c.connection != source.connectionID {
			return pipeline.Delivery{}, fmt.Errorf("invalid pipeline continuation")
		}
		trace = c.trace
	}
	if trace.hops >= pipeline.MaxHops {
		return pipeline.Delivery{}, fmt.Errorf("pipeline hop limit exceeded")
	}
	trace.hops++
	r.Token = string(contract.NewID())
	r.SourceIP, r.SourcePort = trace.ip, trace.port
	h.pipelineTokens[r.Token] = pipelineCapability{target: r.TargetID, connection: target.connectionID, generation: target.identity.CredentialGeneration, trace: trace}
	return pipeline.Delivery{Request: r, SourceID: source.identity.TrapID, TraceID: trace.id, Hop: trace.hops, ExpiresAt: trace.expires}, nil
}
