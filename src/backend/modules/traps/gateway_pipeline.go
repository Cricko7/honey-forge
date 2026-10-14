package traps

import (
	"context"
	"honey-forge/modules/agentws"
	"honey-forge/modules/profiles"
)

func (g *Gateway) PipelineSnapshot(ctx context.Context, identity agentws.Identity, connection string) (profiles.Snapshot, error) {
	return g.repository.PipelineSnapshot(ctx, identity, connection)
}
