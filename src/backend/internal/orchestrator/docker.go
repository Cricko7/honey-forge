package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type DockerCLI struct {
	ClientCAFile      string
	ClientCAKeyFile   string
	ServerPinsFile    string
	Run               func(context.Context, []byte, ...string) ([]byte, error)
	Network           string
	ManagementNetwork string
	WorkerBindIP      string
	ProxyService      string
	WSURL             string
	WSHostIP          string
	RedisURL          string
	CAFile            string
	RedisCAFile       string
	TCPImage          string
	RedisImage        string
	HoneytokenImage   string
	SSHImage          string
	SandboxImage      string
	TetragonContainer string
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
