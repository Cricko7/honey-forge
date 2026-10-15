package decoys

import (
	"context"

	"honey-forge/internal/decoys/conpottrap"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

type Binding struct {
	Port      int
	Transport string
}

// Bindings keeps UDP publication explicit; Ports still reserves port numbers
// conservatively across transports for the existing placement contract.
func Bindings(ctx context.Context, snapshot profiles.Snapshot) ([]Binding, error) {
	ports, err := Ports(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	transport := "tcp"
	if snapshot.TypeID == "conpot" {
		c, err := conpottrap.ParseConfig(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		transport = catalog.ConpotTransport(c.Services[0].Protocol)
	}
	bindings := make([]Binding, 0, len(ports))
	for _, port := range ports {
		bindings = append(bindings, Binding{Port: port, Transport: transport})
	}
	return bindings, nil
}
