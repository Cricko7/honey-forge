// Package dionaeatrap runs the pinned native Dionaea engine in an isolated worker.
package dionaeatrap

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"honey-forge/internal/decoys/internal/enginepipe"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type Config = enginepipe.Config
type Runtime = enginepipe.Runtime

func ParseConfig(ctx context.Context, s profiles.Snapshot) (Config, error) {
	return enginepipe.ParseConfig(ctx, s, "dionaea")
}
func Start(ctx context.Context, s profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (*Runtime, error) {
	c, err := ParseConfig(ctx, s)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("Dionaea requires a Linux isolated worker")
	}
	cmd := exec.Command("/opt/dionaea/bin/dionaea", "-c", "/opt/dionaea/etc/honeyforge.cfg", "-w", "/run/dionaea")
	return enginepipe.Start(ctx, s, c, host, sink, cmd, true)
}
