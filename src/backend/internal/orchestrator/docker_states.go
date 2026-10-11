package orchestrator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// States batches Docker CLI calls so idle cost does not grow with trap count.
func (d DockerCLI) States(ctx context.Context, ids []string) (map[string]ServiceState, error) {
	states := make(map[string]ServiceState, len(ids))
	if len(ids) == 0 {
		return states, nil
	}
	var revision string
	var err error
	if d.ClientCAFile != "" {
		revision, err = d.trustRevision()
		if err != nil {
			return nil, err
		}
	}
	wanted := make(map[string]string, len(ids))
	for _, id := range ids {
		wanted[serviceName(id)] = id
	}
	output, err := d.call(ctx, nil, "service", "ls", "--format", "{{.Name}}")
	if err != nil {
		return nil, fmt.Errorf("list trap services: %w", err)
	}
	args := []string{"service", "inspect", "--format", "{{.Spec.Name}} {{json .Spec.Labels}}"}
	for _, name := range strings.Fields(string(output)) {
		if _, ok := wanted[name]; ok {
			args = append(args, name)
		}
	}
	if len(args) == 4 {
		return states, nil
	}
	output, err = d.call(ctx, nil, args...)
	if err != nil {
		return nil, fmt.Errorf("inspect trap services: %w", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		name, raw, ok := strings.Cut(scanner.Text(), " ")
		id, wantedName := wanted[name]
		if !ok || !wantedName {
			return nil, fmt.Errorf("unexpected trap service metadata")
		}
		var labels map[string]string
		if err := json.Unmarshal([]byte(raw), &labels); err != nil {
			return nil, fmt.Errorf("decode trap labels: %w", err)
		}
		state, err := d.stateFromLabels(labels, id, revision)
		if err != nil {
			return nil, err
		}
		states[id] = state
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read trap metadata: %w", err)
	}
	return states, nil
}
