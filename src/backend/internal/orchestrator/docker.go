package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

type DockerCLI struct {
	Run             func(context.Context, []byte, ...string) ([]byte, error)
	Network         string
	ProxyService    string
	WSURL           string
	RedisURL        string
	CAFile          string
	RedisCAFile     string
	TCPImage        string
	RedisImage      string
	HoneytokenImage string
	SSHImage        string
	SandboxImage    string
}

func runDocker(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w: %s", strings.Join(args[:2], " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (d DockerCLI) call(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	if d.Run != nil {
		return d.Run(ctx, input, args...)
	}
	return runDocker(ctx, input, args...)
}

func (d DockerCLI) Prepare(ctx context.Context) error {
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
	listed, err := d.call(ctx, nil, "network", "ls", "--filter", "name="+d.Network, "--format", "{{.Name}}")
	if err != nil {
		return fmt.Errorf("list decoy networks: %w", err)
	}
	found := false
	for _, name := range strings.Fields(string(listed)) {
		found = found || name == d.Network
	}
	if !found {
		if _, err := d.call(ctx, nil, "network", "create", "--driver", "overlay", "--attachable", d.Network); err != nil {
			return fmt.Errorf("create decoy network: %w", err)
		}
	} else {
		kind, err := d.call(ctx, nil, "network", "inspect", d.Network, "--format", "{{.Driver}} {{.Attachable}}")
		if err != nil {
			return fmt.Errorf("inspect decoy network: %w", err)
		}
		if strings.TrimSpace(string(kind)) != "overlay true" {
			return fmt.Errorf("decoy network %s must be attachable overlay", d.Network)
		}
	}
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
	if _, attached := networks[d.Network]; attached {
		return nil
	}
	if _, err := d.call(ctx, nil, "network", "connect", "--alias", alias, d.Network, ids[0]); err != nil {
		return fmt.Errorf("connect Compose %s to decoy network: %w", service, err)
	}
	return nil
}

func serviceName(id string) string { return "hf-trap-" + id }

func (d DockerCLI) exists(ctx context.Context, id string) (bool, error) {
	output, err := d.call(ctx, nil, "service", "ls", "--filter", "name="+serviceName(id), "--format", "{{.Name}}")
	if err != nil {
		return false, fmt.Errorf("list trap services: %w", err)
	}
	for _, name := range strings.Fields(string(output)) {
		if name == serviceName(id) {
			return true, nil
		}
	}
	return false, nil
}

func (d DockerCLI) Current(ctx context.Context, id string) (ServiceState, error) {
	exists, err := d.exists(ctx, id)
	if err != nil || !exists {
		return ServiceState{}, err
	}
	output, err := d.call(ctx, nil, "service", "inspect", serviceName(id), "--format", "{{json .Spec.Labels}}")
	if err != nil {
		return ServiceState{}, fmt.Errorf("inspect trap service: %w", err)
	}
	var labels map[string]string
	if err := json.Unmarshal(output, &labels); err != nil {
		return ServiceState{}, fmt.Errorf("decode trap service labels: %w", err)
	}
	if labels["honeyforge.trap"] != id {
		return ServiceState{}, fmt.Errorf("service name %s belongs to another owner", serviceName(id))
	}
	generation, err := strconv.ParseInt(labels["honeyforge.generation"], 10, 64)
	if err != nil {
		return ServiceState{}, fmt.Errorf("invalid trap service generation: %w", err)
	}
	var ports []int
	if raw := labels["honeyforge.ports"]; raw != "" {
		for _, part := range strings.Split(raw, ",") {
			port, err := strconv.Atoi(part)
			if err != nil {
				return ServiceState{}, fmt.Errorf("invalid trap service port: %w", err)
			}
			ports = append(ports, port)
		}
	}
	slices.Sort(ports)
	return ServiceState{Exists: true, Generation: generation, Ports: ports}, nil
}

func (d DockerCLI) Remove(ctx context.Context, id string) error {
	exists, err := d.exists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		if _, err := d.call(ctx, nil, "service", "rm", serviceName(id)); err != nil {
			return fmt.Errorf("remove trap service: %w", err)
		}
	}
	return d.removeSecrets(ctx, id)
}

func (d DockerCLI) removeSecrets(ctx context.Context, id string) error {
	output, err := d.call(ctx, nil, "secret", "ls", "--filter", "label=honeyforge.trap="+id, "--format", "{{.Name}}")
	if err != nil {
		return fmt.Errorf("list trap secrets: %w", err)
	}
	for _, name := range strings.Fields(string(output)) {
		if !strings.HasPrefix(name, "hf-") || !strings.Contains(name, id) {
			return fmt.Errorf("unexpected secret name for trap %s", id)
		}
		if _, err := d.call(ctx, nil, "secret", "rm", name); err != nil {
			return fmt.Errorf("remove trap secret: %w", err)
		}
	}
	return nil
}

func (d DockerCLI) createSecret(ctx context.Context, id, name string, value []byte) error {
	if _, err := d.call(ctx, value, "secret", "create", "--label", "honeyforge.trap="+id, name, "-"); err != nil {
		return fmt.Errorf("create trap secret %s: %w", name, err)
	}
	return nil
}
