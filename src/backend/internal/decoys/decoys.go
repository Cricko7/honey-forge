// Package decoys selects the built-in trap runtime for a profile snapshot.
package decoys

import (
	"context"
	"fmt"

	"honey-forge/internal/decoys/honeytrap"

	"honey-forge/internal/decoys/redistrap"
	"honey-forge/internal/decoys/sshtrap"
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

// Runtime registration is the single source for selection, advertised types,
// and exposed ports. Protocol configuration stays in its owning package.
type definition struct {
	typeID  string
	version int32
	start   Starter
	ports   func(context.Context, profiles.Snapshot) ([]int, error)
}

var builtins = []definition{
	{
		typeID: "tcp-banner", version: 1,
		start: func(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (Runtime, error) {
			return tcptrap.Start(ctx, snapshot, host, tcptrap.Sink(sink))
		},
		ports: func(ctx context.Context, snapshot profiles.Snapshot) ([]int, error) {
			config, err := tcptrap.ParseConfig(ctx, snapshot)
			if err != nil {
				return nil, err
			}
			ports := make([]int, 0, len(config.Listeners))
			for _, listener := range config.Listeners {
				ports = append(ports, listener.Port)
			}
			return ports, nil
		},
	},
	{
		typeID: "redis-emulator", version: 1,
		start: func(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (Runtime, error) {
			return redistrap.Start(ctx, snapshot, host, redistrap.Sink(sink))
		},
		ports: func(ctx context.Context, snapshot profiles.Snapshot) ([]int, error) {
			config, err := redistrap.ParseConfig(ctx, snapshot)
			if err != nil {
				return nil, err
			}
			return []int{config.Services[0].Port}, nil
		},
	},
	{
		typeID: "honeytoken-http", version: 1,
		start: func(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (Runtime, error) {
			return honeytrap.Start(ctx, snapshot, host, honeytrap.Sink(sink))
		},
		ports: func(ctx context.Context, snapshot profiles.Snapshot) ([]int, error) {
			config, err := honeytrap.ParseConfig(ctx, snapshot)
			if err != nil {
				return nil, err
			}
			return []int{config.Services[0].Port}, nil
		},
	},
	{
		typeID: "ssh-high-interaction", version: 1,
		start: func(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (Runtime, error) {
			return sshtrap.Start(ctx, snapshot, host, sshtrap.Sink(sink))
		},
		ports: func(ctx context.Context, snapshot profiles.Snapshot) ([]int, error) {
			config, err := sshtrap.ParseConfig(ctx, snapshot)
			if err != nil {
				return nil, err
			}
			return []int{config.Services[0].Port}, nil
		},
	},
}

func lookup(snapshot profiles.Snapshot) (definition, error) {
	for _, entry := range builtins {
		if entry.typeID == snapshot.TypeID && entry.version == snapshot.TypeVersion {
			return entry, nil
		}
	}
	return definition{}, fmt.Errorf("unsupported trap type %q version %d", snapshot.TypeID, snapshot.TypeVersion)
}

func Select(snapshot profiles.Snapshot) (Starter, error) {
	entry, err := lookup(snapshot)
	return entry.start, err
}

func SupportedTypes() []agentws.SupportedType {
	types := make([]agentws.SupportedType, 0, len(builtins))
	for _, entry := range builtins {
		types = append(types, agentws.SupportedType{TypeID: entry.typeID, TypeVersion: entry.version, Actions: []string{"start", "stop", "apply_config"}})
	}
	return types
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
	entry, err := lookup(snapshot)
	if err != nil {
		return nil, err
	}
	return entry.ports(ctx, snapshot)
}
