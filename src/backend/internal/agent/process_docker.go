package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"honey-forge/internal/contract"
	"honey-forge/internal/decoys"
	"honey-forge/modules/profiles"
)

func (p *Process) workerName() string {
	return "nm-worker-" + p.TrapID
}

func (p *Process) workerArgs(ctx context.Context, snapshot profiles.Snapshot) ([]string, error) {
	if p.WorkerImage == "" || p.WorkerNetwork == "" || p.WorkerNetwork == "host" || p.WorkerNetwork == "none" || !contract.ValidID(p.TrapID) {
		return nil, fmt.Errorf("isolated worker image, network and trap ID are required")
	}

	ip := p.WorkerBindIP
	if ip == "" {
		ip = "0.0.0.0"
	}

	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("NM_BIND_IP must be an IP address")
	}

	ports, err := decoys.Ports(ctx, snapshot)
	if err != nil {
		return nil, fmt.Errorf("worker ports: %w", err)
	}

	args := []string{"run", "--rm", "--interactive", "--init", "--pull=never", "--name", p.workerName(),
		"--log-driver", "local", "--log-opt", "max-size=10m", "--log-opt", "max-file=3",
		"--label", "com.nodemetrics.worker=" + p.TrapID,
		"--network", p.WorkerNetwork, "--hostname", "service", "--dns", "127.0.0.1", "--dns-search", ".",
		"--ipc", "private", "--cgroupns", "private",
		"--ulimit", "core=0:0", "--read-only", "--cap-drop", "ALL",
		"--tmpfs", "/run:rw,nosuid,nodev,noexec,size=1m,mode=0755",
		// The entrypoint installs an egress firewall, then drops to UID 65532.
		// SETUID clears capabilities; no-new-privileges prevents regaining them.
		// The root init needs KILL to forward shutdown signals across the UID drop.
		"--cap-add", "NET_ADMIN", "--cap-add", "SETUID", "--cap-add", "SETGID", "--cap-add", "KILL",
		"--security-opt", "no-new-privileges", "--user", "0:0",
		"--sysctl", "net.ipv4.ip_unprivileged_port_start=0",
		"--sysctl", "net.ipv6.conf.all.disable_ipv6=1",
		"--memory", "128m", "--memory-swap", "128m", "--pids-limit", "128",
		"--env", "NM_STDIO=1"}
	for _, port := range ports {
		args = append(args, "--publish", net.JoinHostPort(ip, strconv.Itoa(port))+":"+strconv.Itoa(port)+"/tcp")
	}

	return append(args, p.WorkerImage, "--worker"), nil
}

// A restart only removes a container bearing this trap's ownership label.
func (p *Process) removeWorker(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, "docker", "ps", "--all", "--filter", "label=com.nodemetrics.worker="+p.TrapID, "--format", "{{.ID}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("list old trap worker: %w", err)
	}

	for _, id := range strings.Fields(string(output)) {
		if _, err := exec.CommandContext(ctx, "docker", "rm", "--force", id).CombinedOutput(); err != nil {
			return fmt.Errorf("remove old trap worker: %w", err)
		}
	}

	return nil
}

type telemetryBridge struct {
	conn     net.Conn
	output   io.ReadCloser
	input    io.WriteCloser
	once     sync.Once
	closeErr error // published by once
}

func (b *telemetryBridge) Close() error {
	b.once.Do(func() {
		b.closeErr = errors.Join(b.conn.Close(), b.output.Close(), b.input.Close())
	})
	return b.closeErr
}

func (p *Process) startBridge(ctx context.Context, output io.ReadCloser, input io.WriteCloser) error {
	u, err := url.Parse(p.URL)
	if err != nil {
		return fmt.Errorf("local telemetry URL: %w", errors.Join(err, output.Close()))
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return fmt.Errorf("local telemetry socket: %w", errors.Join(err, output.Close()))
	}

	b := &telemetryBridge{conn: conn, output: output, input: input}
	p.bridge = b
	go b.copy(input, conn)
	go b.copy(conn, output)

	return nil
}

func (b *telemetryBridge) copy(dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	// Closing either direction intentionally interrupts the other direction.
	if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
		slog.Debug("worker telemetry pipe interrupted", "error", err)
	}

	if err := b.Close(); err != nil {
		slog.Debug("worker telemetry pipe closed", "error", err)
	}
}
