package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"honey-forge/internal/platform/tlsconfig"
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

	if image == "" || d.Network == "" || d.ManagementNetwork == "" || d.Network == d.ManagementNetwork || d.WSURL == "" || d.RedisURL == "" || token == "" {
		return fmt.Errorf("incomplete trap deployment configuration")
	}

	if err := d.removeSecrets(ctx, target.ID); err != nil {
		return err
	}

	base := fmt.Sprintf("nm-%s-%d", target.ID, generation)
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
		"--label", "com.nodemetrics.instance=" + target.ID,
		"--label", "com.nodemetrics.generation=" + strconv.FormatInt(generation, 10),
		"--label", "com.nodemetrics.ports=" + portLabel(ports),
		"--label", "com.nodemetrics.isolation=mask3",
		"--replicas", "1", "--network", d.ManagementNetwork, "--hostname", "{{.Node.Hostname}}", "--init", "--read-only", "--cap-drop", "ALL", "--cap-add", "SETUID", "--cap-add", "SETGID",
		"--sysctl", "net.ipv4.ip_unprivileged_port_start=0",
		"--limit-memory", memory, "--reserve-memory", "64m", "--reserve-cpu", "0.1", "--limit-pids", "128", "--user", "0:0",
		"--restart-condition", "on-failure",
		"--secret", "source=" + base + "-token,target=nm_token,uid=0,gid=0,mode=0400",
		"--secret", "source=" + base + "-redis,target=store_url,uid=0,gid=0,mode=0400",
		"--env", "NM_INSTANCE_ID=" + target.ID,
		"--env", "NM_TOKEN_FILE=/run/secrets/nm_token",
		"--env", "NM_STORE_URL_FILE=/run/secrets/store_url",
		"--env", "NM_WS_URL=" + d.WSURL}
	if target.DeploymentNodeID != "" {
		args = append(args, "--constraint", "node.id=="+target.DeploymentNodeID, "--constraint", "node.role==worker", "--constraint", "node.labels.honeyforge.organization=="+target.OrganizationID, "--label", "com.nodemetrics.node="+target.DeploymentNodeID)
	} else {
		args = append(args, "--constraint", "node.role==manager")
	}

	if d.ClientCAFile != "" {
		revision, err := d.trustRevision()
		if err != nil {
			return err
		}

		certificate, key, expires, err := tlsconfig.IssueClient(d.ClientCAFile, d.ClientCAKeyFile, target.ID)
		if err != nil {
			return err
		}

		pins, err := os.ReadFile(d.ServerPinsFile)
		if err != nil {
			return fmt.Errorf("read server pins: %w", err)
		}

		for _, material := range []struct {
			suffix, target, variable string
			data                     []byte
		}{
			{"-cert", "nm_cert", "NM_CERT_FILE", certificate},
			{"-key", "nm_key", "NM_KEY_FILE", key},
			{"-pins", "nm_server_pins", "NM_SERVER_PINS_FILE", pins},
		} {
			if err := d.createSecret(ctx, target.ID, base+material.suffix, material.data); err != nil {
				return err
			}

			args = append(args, "--secret", "source="+base+material.suffix+",target="+material.target+",uid=0,gid=0,mode=0400", "--env", material.variable+"=/run/secrets/"+material.target)
		}

		args = append(args, "--label", "com.nodemetrics.certificate-expires="+expires.UTC().Format(time.RFC3339))
		args = append(args, "--label", "com.nodemetrics.tls-trust="+revision)
	}

	for _, trust := range []struct{ file, suffix, target, variable string }{
		{d.CAFile, "-ca", "nm_ca", "NM_CA_FILE"},
		{d.RedisCAFile, "-redis-ca", "store_ca", "NM_STORE_CA_FILE"},
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

		if target.Snapshot.TypeID == "ssh-high-interaction" {
			args = append(args, "--publish", fmt.Sprintf("published=%d,target=%d,protocol=tcp,mode=host", port, port))
		}
	}

	// Only the trusted supervisor can use Docker. Decoy workers receive neither
	// this socket nor management credentials, and communicate through a TLS pipe.
	args = append(args, "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock")
	if target.Snapshot.TypeID == "ssh-high-interaction" {
		args = append(args, "--env", "NM_SANDBOX_IMAGE="+d.SandboxImage,
			"--limit-cpu", "1")
	} else {
		args = append(args, "--env", "NM_WORKER_IMAGE="+image, "--env", "NM_WORKER_NETWORK="+d.Network)
		if target.DeploymentNodeID != "" {
			args = append(args, "--env", "NM_BIND_IP=0.0.0.0")
		} else if d.WorkerBindIP != "" {
			args = append(args, "--env", "NM_BIND_IP="+d.WorkerBindIP)
		}
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
