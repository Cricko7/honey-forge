package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"honey-forge/internal/contract"
)

// CleanupSandboxes enforces a host-side TTL even after a gateway crashes or an
// attacker stops the sandbox's sleep process. Labels are outside attacker access.
func (d DockerCLI) CleanupSandboxes(ctx context.Context, now time.Time) error {
	output, err := d.call(ctx, nil, "ps", "--all", "--filter", "label=honeyforge.sandbox=true", "--format", "{{.ID}}")
	if err != nil {
		return fmt.Errorf("list SSH sandboxes: %w", err)
	}
	for _, id := range strings.Fields(string(output)) {
		info, err := d.call(ctx, nil, "container", "inspect", id, "--format", `{{.Name}} {{index .Config.Labels "honeyforge.expires"}}`)
		if err != nil {
			return fmt.Errorf("inspect SSH sandbox: %w", err)
		}
		fields := strings.Fields(string(info))
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "/hf-session-") || !contract.ValidID(strings.TrimPrefix(fields[0], "/hf-session-")) {
			return fmt.Errorf("invalid owned SSH sandbox metadata")
		}
		expires, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || expires < 1 {
			return fmt.Errorf("invalid SSH sandbox expiry")
		}
		if now.Unix() >= expires {
			if _, err := d.call(ctx, nil, "rm", "--force", id); err != nil {
				return fmt.Errorf("remove expired SSH sandbox: %w", err)
			}
		}
	}
	return nil
}
