package agentws

import (
	"context"
	"slices"

	"honey-forge/internal/contract"
	"honey-forge/internal/platform/traffic"
)

type trafficDelivery struct {
	delivery traffic.Delivery
	source   *agentSession
}

func (h *Handler) trafficSourceAuthorized(item trafficDelivery) bool {
	entry := h.registry.lookup(item.source.identity.TrapID)
	if entry == nil {
		return false
	}

	return h.withActive(entry, item.source, func(ctx context.Context) error {
		targets, err := h.TrafficTargets(ctx, item.source.identity.OrganizationID, item.source.identity.TrapID)
		if err != nil {
			return err
		}
		if !slices.Contains(targets, item.delivery.TargetID) {
			return contract.NewError("forbidden")
		}
		return nil
	}) == nil
}

func (h *Handler) handleTraffic(entry *sessionEntry, session *agentSession, message contract.Envelope) error {
	if h.TrafficTargets == nil || !session.trafficEnabled {
		return session.replyError(message, contract.NewError("invalid_message"))
	}

	var frame traffic.Frame
	if err := decodePayload(message.Payload, &frame); err != nil {
		return session.replyError(message, err)
	}

	if err := traffic.CheckFrame(frame); err != nil {
		return session.replyError(message, err)
	}

	// Release the source lock before visiting destinations: A -> B and B -> A
	// may arrive concurrently and must never hold each other's session locks.
	if err := h.withActive(entry, session, func(context.Context) error {
		return nil
	}); err != nil {
		return err
	}

	sent, skipped, err := h.routeTraffic(session, frame)
	if err != nil {
		return session.replyError(message, err)
	}

	return session.socket.Reply(session.ctx, message, "traffic.ack", map[string]any{"connection_id": frame.ConnectionID, "sequence": frame.Sequence, "queued_targets": sent, "skipped_targets": skipped})
}

func (h *Handler) routeTraffic(source *agentSession, frame traffic.Frame) (int, int, error) {
	targets, err := h.TrafficTargets(source.ctx, source.identity.OrganizationID, source.identity.TrapID)
	if err != nil {
		return 0, 0, err
	}

	sent, skipped := 0, 0
	for _, id := range targets {
		entry := h.registry.lookup(id)
		if entry == nil {
			skipped++
			continue
		}

		entry.mu.Lock()
		target := entry.current
		if target != nil && target.active.Load() && target.trafficEnabled && target.identity.OrganizationID == source.identity.OrganizationID {
			select {
			case target.traffic <- trafficDelivery{traffic.Delivery{SourceID: source.identity.TrapID, TargetID: id, Frame: frame}, source}:
				sent++
			default:
				skipped++
			}
		} else {
			skipped++
		}

		entry.mu.Unlock()
	}

	return sent, skipped, nil
}

// lookup does not create registry entries for offline traffic destinations.
func (r *sessionRegistry) lookup(id string) *sessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.entries[id]
}
