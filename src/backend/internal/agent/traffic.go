package agent

import (
	"context"
	"log/slog"

	"honey-forge/internal/platform/traffic"
)

func (l *Local) acceptTraffic(frame traffic.Frame) error {
	if err := traffic.CheckFrame(frame); err != nil {
		return err
	}

	frame.Data = append([]byte(nil), frame.Data...)
	select {
	case l.Traffic <- frame:
	default:
		l.TrafficDropped.Add(1)
	}

	return nil
}

// Mirror is non-blocking: a slow/disconnected control plane cannot stall the decoy.
func (c *LocalClient) Mirror(frame traffic.Frame) {
	select {
	case c.traffic <- frame:
	case <-c.done:
	default:
		c.TrafficDropped.Add(1)
	}
}

func (c *LocalClient) sendTraffic() {
	for {
		select {
		case <-c.done:
			return
		case frame := <-c.traffic:
			if err := c.exchange(context.Background(), map[string]any{"type": "trap.traffic", "frame": frame}, "traffic_connection_id", frame.ConnectionID); err != nil {
				slog.Debug("raw traffic mirror interrupted")
				return
			}
		}
	}
}
