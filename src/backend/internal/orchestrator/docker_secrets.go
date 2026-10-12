package orchestrator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

func (d DockerCLI) trustRevision() (string, error) {
	hash := sha256.New()
	for _, file := range []string{d.CAFile, d.ClientCAFile, d.ServerPinsFile} {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read agent trust configuration: %w", err)
		}

		if _, err := hash.Write(raw); err != nil {
			return "", fmt.Errorf("hash agent trust configuration: %w", err)
		}
	}

	// Rotate immutable agent configuration when a local endpoint mapping changes.
	if d.WSHostIP != "" {
		fmt.Fprintf(hash, "\nws-host:%s:%s\n", d.WSURL, d.WSHostIP)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
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

	workers, err := d.call(ctx, nil, "ps", "--all", "--filter", "label=com.nodemetrics.worker="+id, "--format", "{{.ID}}")
	if err != nil {
		return fmt.Errorf("list trap workers: %w", err)
	}

	for _, worker := range strings.Fields(string(workers)) {
		if _, err := d.call(ctx, nil, "rm", "--force", worker); err != nil {
			return fmt.Errorf("remove trap worker: %w", err)
		}
	}

	return d.removeSecrets(ctx, id)
}

func (d DockerCLI) removeSecrets(ctx context.Context, id string) error {
	output, err := d.call(ctx, nil, "secret", "ls", "--filter", "label=com.nodemetrics.instance="+id, "--format", "{{.Name}}")
	if err != nil {
		return fmt.Errorf("list trap secrets: %w", err)
	}

	for _, name := range strings.Fields(string(output)) {
		if !strings.HasPrefix(name, "nm-") || !strings.Contains(name, id) {
			return fmt.Errorf("unexpected secret name for trap %s", id)
		}

		if _, err := d.call(ctx, nil, "secret", "rm", name); err != nil {
			return fmt.Errorf("remove trap secret: %w", err)
		}
	}

	return nil
}

func (d DockerCLI) createSecret(ctx context.Context, id, name string, value []byte) error {
	if _, err := d.call(ctx, value, "secret", "create", "--label", "com.nodemetrics.instance="+id, name, "-"); err != nil {
		return fmt.Errorf("create trap secret %s: %w", name, err)
	}

	return nil
}
