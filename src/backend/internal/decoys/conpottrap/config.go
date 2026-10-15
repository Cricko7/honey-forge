package conpottrap

import (
	"context"

	"honey-forge/internal/decoys/internal/enginepipe"
	"honey-forge/modules/profiles"
)

type Config = enginepipe.Config

func ParseConfig(ctx context.Context, s profiles.Snapshot) (Config, error) {
	return enginepipe.ParseConfig(ctx, s, "conpot")
}
