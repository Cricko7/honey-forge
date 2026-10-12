package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

func serviceName(id string) string {
	return "node-metrics-" + id
}

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

	var revision string
	if d.ClientCAFile != "" {
		revision, err = d.trustRevision()
		if err != nil {
			return ServiceState{}, err
		}
	}

	return d.stateFromLabels(labels, id, revision)
}

func (d DockerCLI) stateFromLabels(labels map[string]string, id, revision string) (ServiceState, error) {
	if labels["com.nodemetrics.instance"] != id {
		return ServiceState{}, fmt.Errorf("service name %s belongs to another owner", serviceName(id))
	}

	generation, err := strconv.ParseInt(labels["com.nodemetrics.generation"], 10, 64)
	if err != nil {
		return ServiceState{}, fmt.Errorf("invalid trap service generation: %w", err)
	}

	var ports []int
	if raw := labels["com.nodemetrics.ports"]; raw != "" {
		for _, part := range strings.Split(raw, ",") {
			port, err := strconv.Atoi(part)
			if err != nil {
				return ServiceState{}, fmt.Errorf("invalid trap service port: %w", err)
			}

			ports = append(ports, port)
		}
	}

	slices.Sort(ports)
	var expires time.Time
	if raw := labels["com.nodemetrics.certificate-expires"]; raw != "" {
		expires, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return ServiceState{}, fmt.Errorf("invalid certificate expiry: %w", err)
		}
	} else if d.ClientCAFile != "" {
		// Replace tasks deployed before mTLS was introduced.
		expires = time.Unix(1, 0)
	}

	if d.ClientCAFile != "" {
		if labels["com.nodemetrics.tls-trust"] != revision {
			expires = time.Unix(1, 0)
		}
	}

	return ServiceState{
		DeploymentNodeID:   labels["com.nodemetrics.node"],
		Exists:             true,
		Generation:         generation,
		Ports:              ports,
		CertificateExpires: expires,
		UnsafeNetwork:      labels["com.nodemetrics.isolation"] != "mask3",
	}, nil
}
