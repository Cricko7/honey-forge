package sshtrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"honey-forge/internal/contract"
)

// backend is consumed by the gateway; tests supply a real SSH peer without Docker.
type backend interface {
	Open(context.Context, string, int) (*ssh.Client, func() error, error)
}

type dockerBackend struct{ image string }

func sandboxArgs(id, image, publicKey string, seconds int) ([]string, error) {
	if !contract.ValidID(id) || image == "" || seconds < 10 || seconds > 3600 {
		return nil, fmt.Errorf("invalid sandbox configuration")
	}
	return []string{"run", "--detach", "--rm", "--init", "--pull=never", "--name", "hf-session-" + id,
		"--label", "honeyforge.sandbox=true", "--label", "honeyforge.expires=" + strconv.FormatInt(time.Now().Add(time.Duration(seconds)*time.Second).Unix(), 10),
		"--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", "128m", "--memory-swap", "128m", "--cpus", "0.5", "--pids-limit", "64", "--user", "1000:1000",
		"--tmpfs", "/home/sandbox:rw,nosuid,nodev,size=32m,uid=1000,gid=1000,mode=0700",
		"--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=16m,mode=1777", "--env", "SANDBOX_PUBLIC_KEY=" + publicKey,
		image, strconv.Itoa(seconds)}, nil
}

func dockerCall(ctx context.Context, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sandbox docker command: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (d dockerBackend) Open(ctx context.Context, id string, seconds int) (*ssh.Client, func() error, error) {
	startup, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate sandbox key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, nil, err
	}
	args, err := sandboxArgs(id, d.image, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), seconds)
	if err != nil {
		return nil, nil, err
	}
	if _, err := dockerCall(startup, args...); err != nil {
		return nil, nil, err
	}
	name := "hf-session-" + id
	cleanup := func() error {
		removeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		output, err := exec.CommandContext(removeCtx, "docker", "rm", "--force", name).CombinedOutput()
		if err != nil && !strings.Contains(string(output), "No such container") {
			return fmt.Errorf("remove sandbox: %w", err)
		}
		return nil
	}
	fail := func(err error) (*ssh.Client, func() error, error) {
		if removeErr := cleanup(); removeErr != nil {
			return nil, nil, fmt.Errorf("%w; cleanup: %w", err, removeErr)
		}
		return nil, nil, err
	}
	// The image entrypoint creates an ephemeral host key before serving inetd SSH.
	for {
		if _, err := dockerCall(startup, "exec", name, "test", "-s", "/home/sandbox/.ssh/ready"); err == nil {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-startup.Done():
			timer.Stop()
			return fail(startup.Err())
		case <-timer.C:
		}
	}
	pipe, err := startPipe(ctx, "exec", "--interactive", name, "/usr/sbin/sshd", "-i", "-e", "-f", "/etc/ssh/sandbox_config")
	if err != nil {
		return fail(err)
	}
	if err := pipe.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		pipe.Close()
		return fail(err)
	}
	// security: transport is a Docker exec pipe to the exact container we created,
	// never a network address supplied by an attacker. No reusable private key
	// or control-plane credential enters the sandbox.
	conn, channels, requests, err := ssh.NewClientConn(pipe, "sandbox", &ssh.ClientConfig{User: "sandbox", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		pipe.Close()
		return fail(fmt.Errorf("connect sandbox SSH: %w", err))
	}
	if err := pipe.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return fail(err)
	}
	client := ssh.NewClient(conn, channels, requests)
	return client, cleanup, nil
}
