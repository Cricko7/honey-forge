package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func (d DockerCLI) Deploy(ctx context.Context, target Target, token string, generation int64, ports []int) (err error) {
	image := ""
	switch {
	case target.Snapshot.TypeID == "tcp-banner" && target.Snapshot.TypeVersion == 1:
		image = d.TCPImage
	case target.Snapshot.TypeID == "redis-emulator" && target.Snapshot.TypeVersion == 1:
		image = d.RedisImage
	case target.Snapshot.TypeID == "honeytoken-http" && target.Snapshot.TypeVersion == 1:
		image = d.HoneytokenImage
	case target.Snapshot.TypeID == "ssh-high-interaction" && target.Snapshot.TypeVersion == 1:
		image = d.SSHImage
		if d.SandboxImage == "" {
			return fmt.Errorf("SSH sandbox image is required")
		}
	}
	if image == "" || d.Network == "" || d.WSURL == "" || d.RedisURL == "" || token == "" {
		return fmt.Errorf("incomplete trap deployment configuration")
	}
	if err := d.removeSecrets(ctx, target.ID); err != nil {
		return err
	}
	base := fmt.Sprintf("hf-%s-%d", target.ID, generation)
	cleanup := true
	defer func() {
		if cleanup {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if cleanupErr := d.removeSecrets(cleanupCtx, target.ID); cleanupErr != nil {
				err = fmt.Errorf("%w; cleanup: %w", err, cleanupErr)
			}
		}
	}()
	if err := d.createSecret(ctx, target.ID, base+"-token", []byte(token)); err != nil {
		return err
	}
	if err := d.createSecret(ctx, target.ID, base+"-redis", []byte(d.RedisURL)); err != nil {
		return err
	}
	memory := "128m"
	if target.Snapshot.TypeID == "ssh-high-interaction" {
		memory = "512m"
	}
	args := []string{"service", "create", "--detach=true", "--name", serviceName(target.ID),
		"--label", "honeyforge.trap=" + target.ID,
		"--label", "honeyforge.generation=" + strconv.FormatInt(generation, 10),
		"--label", "honeyforge.ports=" + portLabel(ports),
		"--replicas", "1", "--network", d.Network, "--hostname", "{{.Node.Hostname}}", "--init", "--read-only", "--cap-drop", "ALL", "--cap-add", "SETUID", "--cap-add", "SETGID",
		"--sysctl", "net.ipv4.ip_unprivileged_port_start=0",
		"--limit-memory", memory, "--reserve-memory", "64m", "--reserve-cpu", "0.1", "--limit-pids", "128", "--user", "0:0",
		"--restart-condition", "on-failure",
		"--secret", "source=" + base + "-token,target=agent_token,uid=0,gid=0,mode=0400",
		"--secret", "source=" + base + "-redis,target=redis_url,uid=0,gid=0,mode=0400",
		"--env", "AGENT_TRAP_ID=" + target.ID,
		"--env", "AGENT_TOKEN_FILE=/run/secrets/agent_token",
		"--env", "AGENT_REDIS_URL_FILE=/run/secrets/redis_url",
		"--env", "AGENT_WS_URL=" + d.WSURL}
	for _, trust := range []struct{ file, suffix, target, variable string }{
		{d.CAFile, "-ca", "agent_ca", "AGENT_CA_FILE"},
		{d.RedisCAFile, "-redis-ca", "redis_ca", "AGENT_REDIS_CA_FILE"},
	} {
		if trust.file == "" {
			continue
		}
		ca, err := os.ReadFile(trust.file)
		if err != nil {
			return fmt.Errorf("read agent CA: %w", err)
		}
		if err := d.createSecret(ctx, target.ID, base+trust.suffix, ca); err != nil {
			return err
		}
		args = append(args, "--secret", "source="+base+trust.suffix+",target="+trust.target+",uid=0,gid=0,mode=0400", "--env", trust.variable+"=/run/secrets/"+trust.target)
	}
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid trap port %d", port)
		}
		args = append(args, "--publish", fmt.Sprintf("published=%d,target=%d,protocol=tcp,mode=host", port, port))
	}
	if target.Snapshot.TypeID == "ssh-high-interaction" {
		args = append(args, "--constraint", "node.role==manager",
			"--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
			"--env", "SSH_SANDBOX_IMAGE="+d.SandboxImage,
			"--limit-cpu", "1")
	}
	args = append(args, image)
	if _, err := d.call(ctx, nil, args...); err != nil {
		return fmt.Errorf("create trap service: %w", err)
	}
	cleanup = false
	return nil
}

func portLabel(ports []int) string {
	parts := make([]string, len(ports))
	for i, port := range ports {
		parts[i] = strconv.Itoa(port)
	}
	return strings.Join(parts, ",")
}
