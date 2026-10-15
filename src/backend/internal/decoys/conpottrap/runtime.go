package conpottrap

import (
	"context"
	_ "embed"
	"os"
	"os/exec"

	"honey-forge/internal/decoys/internal/enginepipe"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

//go:embed scripts/runner.py
var program string

//go:embed scripts/telemetry.py
var telemetryProgram string

//go:embed scripts/network.py
var networkProgram string

type Runtime = enginepipe.Runtime

func Start(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (*Runtime, error) {
	c, err := ParseConfig(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	python := os.Getenv("HF_PYTHON")
	if python == "" {
		python = "python3"
	}
	cmd := exec.Command(python, "-I", "-B", "-u", "-c", enginepipe.ChannelProgram+"\n"+telemetryProgram+"\n"+networkProgram+"\n"+program)
	return enginepipe.Start(ctx, snapshot, c, host, sink, cmd, false)
}
