// Package decoys selects the built-in trap runtime for a profile snapshot.
package decoys

import (
	"context"
	"fmt"

	"honey-forge/internal/decoys/honeytrap"

	"honey-forge/internal/decoys/redistrap"
	"honey-forge/internal/decoys/tcptrap"
	"honey-forge/modules/agentws"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type Runtime interface {
	Stop()
	Err() error
}

type Starter func(context.Context, profiles.Snapshot, string, func(context.Context, events.AgentEvent) error) (Runtime, error)

func Select(snapshot profiles.Snapshot) (Starter, error) {
	switch {
	case snapshot.TypeID == "honeytoken-http" && snapshot.TypeVersion == 1:
		return func(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (Runtime, error) {
			return honeytrap.Start(ctx, snapshot, host, honeytrap.Sink(sink))
		}, nil
	case snapshot.TypeID == "tcp-banner" && snapshot.TypeVersion == 1:
		return func(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (Runtime, error) {
			return tcptrap.Start(ctx, snapshot, host, tcptrap.Sink(sink))
		}, nil
	case snapshot.TypeID == "redis-emulator" && snapshot.TypeVersion == 1:
		return func(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (Runtime, error) {
			return redistrap.Start(ctx, snapshot, host, redistrap.Sink(sink))
		}, nil
	default:
		return nil, fmt.Errorf("unsupported trap type %q version %d", snapshot.TypeID, snapshot.TypeVersion)
	}
}

func SupportedTypes() []agentws.SupportedType {
	return []agentws.SupportedType{
		{TypeID: "tcp-banner", TypeVersion: 1, Actions: []string{"start", "stop", "apply_config"}},
		{TypeID: "redis-emulator", TypeVersion: 1, Actions: []string{"start", "stop", "apply_config"}},
		{TypeID: "honeytoken-http", TypeVersion: 1, Actions: []string{"start", "stop", "apply_config"}},
	}
}

func ForType(typeID string) ([]agentws.SupportedType, error) {
	all := SupportedTypes()
	if typeID == "" {
		return all, nil
	}
	for _, supported := range all {
		if supported.TypeID == typeID {
			return []agentws.SupportedType{supported}, nil
		}
	}
	return nil, fmt.Errorf("unsupported decoy image type %q", typeID)
}

// Ports validates the exact snapshot and returns ports exposed by its runtime.
func Ports(ctx context.Context, snapshot profiles.Snapshot) ([]int, error) {
	switch {
	case snapshot.TypeID == "honeytoken-http" && snapshot.TypeVersion == 1:
		config, err := honeytrap.ParseConfig(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		return []int{config.Services[0].Port}, nil
	case snapshot.TypeID == "tcp-banner" && snapshot.TypeVersion == 1:
		config, err := tcptrap.ParseConfig(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		ports := make([]int, 0, len(config.Listeners))
		for _, listener := range config.Listeners {
			ports = append(ports, listener.Port)
		}
		return ports, nil
	case snapshot.TypeID == "redis-emulator" && snapshot.TypeVersion == 1:
		config, err := redistrap.ParseConfig(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		return []int{config.Services[0].Port}, nil
	default:
		return nil, fmt.Errorf("unsupported trap type %q version %d", snapshot.TypeID, snapshot.TypeVersion)
	}
}
