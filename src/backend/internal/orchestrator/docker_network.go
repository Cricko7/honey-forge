package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

func (d DockerCLI) Prepare(ctx context.Context) error {
	if d.ManagementNetwork == "" || d.Network == "" || d.ManagementNetwork == d.Network || d.Network == "host" || d.ManagementNetwork == "host" {
		return fmt.Errorf("distinct DECOY_NETWORK and MGMT_NETWORK are required")
	}

	if err := d.CleanupSandboxes(ctx, time.Now()); err != nil {
		return err
	}

	state, err := d.call(ctx, nil, "info", "--format", "{{.Swarm.LocalNodeState}}")
	if err != nil {
		return fmt.Errorf("inspect Docker Swarm: %w", err)
	}

	switch strings.TrimSpace(string(state)) {
	case "inactive":
		if _, err := d.call(ctx, nil, "swarm", "init"); err != nil {
			return fmt.Errorf("initialize single-node Docker Swarm: %w", err)
		}
	case "active":
	default:
		return fmt.Errorf("Docker Swarm cannot be used in state %q", strings.TrimSpace(string(state)))
	}

	if _, err := d.call(ctx, nil, "node", "ls", "--format", "{{.ID}}"); err != nil {
		return fmt.Errorf("orchestrator must run on a Swarm manager: %w", err)
	}

	for _, network := range []string{d.Network, d.ManagementNetwork} {
		if err := d.ensureNetwork(ctx, network); err != nil {
			return err
		}
	}

	return d.RefreshConnections(ctx)
}

// RefreshConnections reattaches replaced Compose peers without repeating Swarm bootstrap.
func (d DockerCLI) RefreshConnections(ctx context.Context) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get orchestrator container ID: %w", err)
	}

	project, err := d.call(ctx, nil, "container", "inspect", hostname, "--format", `{{index .Config.Labels "com.docker.compose.project"}}`)
	if err != nil {
		return fmt.Errorf("identify Compose project: %w", err)
	}

	if strings.TrimSpace(string(project)) == "" {
		return fmt.Errorf("orchestrator must run in Docker Compose")
	}

	for _, service := range []string{"api", "redis"} {
		if err := d.connectComposeService(ctx, strings.TrimSpace(string(project)), service, service); err != nil {
			return err
		}
	}

	if d.ProxyService != "" {
		endpoint, err := url.Parse(d.WSURL)
		if err != nil || endpoint.Hostname() == "" {
			return fmt.Errorf("resource proxy requires an agent URL with a host")
		}

		if err := d.connectComposeService(ctx, strings.TrimSpace(string(project)), d.ProxyService, endpoint.Hostname()); err != nil {
			return err
		}
	}

	return nil
}

func (d DockerCLI) ensureNetwork(ctx context.Context, network string) error {
	listed, err := d.call(ctx, nil, "network", "ls", "--filter", "name="+network, "--format", "{{.Name}}")
	if err != nil {
		return fmt.Errorf("list decoy networks: %w", err)
	}

	found := false
	for _, name := range strings.Fields(string(listed)) {
		found = found || name == network
	}

	if !found {
		if _, err := d.call(ctx, nil, "network", "create", "--driver", "overlay", "--attachable", network); err != nil {
			return fmt.Errorf("create decoy network: %w", err)
		}
	} else {
		kind, err := d.call(ctx, nil, "network", "inspect", network, "--format", "{{.Driver}} {{.Attachable}}")
		if err != nil {
			return fmt.Errorf("inspect decoy network: %w", err)
		}

		if strings.TrimSpace(string(kind)) != "overlay true" {
			return fmt.Errorf("network %s must be attachable overlay", network)
		}
	}

	return nil
}

func (d DockerCLI) connectComposeService(ctx context.Context, project, service, alias string) error {
	output, err := d.call(ctx, nil, "ps", "--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service="+service, "--format", "{{.ID}}")
	if err != nil {
		return fmt.Errorf("find Compose %s: %w", service, err)
	}

	ids := strings.Fields(string(output))
	if len(ids) != 1 {
		return fmt.Errorf("expected one running Compose %s container, found %d", service, len(ids))
	}

	joined, err := d.call(ctx, nil, "inspect", ids[0], "--format", "{{json .NetworkSettings.Networks}}")
	if err != nil {
		return fmt.Errorf("inspect Compose %s networks: %w", service, err)
	}

	var networks map[string]json.RawMessage
	if err := json.Unmarshal(joined, &networks); err != nil {
		return fmt.Errorf("decode Compose %s networks: %w", service, err)
	}

	if _, attached := networks[d.ManagementNetwork]; !attached {
		if _, err := d.call(ctx, nil, "network", "connect", "--alias", alias, d.ManagementNetwork, ids[0]); err != nil {
			return fmt.Errorf("connect Compose %s to management network: %w", service, err)
		}
	}

	if _, attached := networks[d.Network]; attached {
		if _, err := d.call(ctx, nil, "network", "disconnect", d.Network, ids[0]); err != nil {
			return fmt.Errorf("disconnect Compose %s from decoy network: %w", service, err)
		}
	}

	return nil
}
