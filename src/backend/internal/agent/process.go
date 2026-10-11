package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"honey-forge/modules/profiles"
)

// Process uses this executable's private child mode. Configuration and the
// short-lived loopback credential never appear in command-line arguments.
type Process struct {
	Executable    string
	URL           string
	Certificate   []byte
	Local         *Local
	WorkerImage   string
	WorkerNetwork string
	WorkerBindIP  string
	TrapID        string
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	done          chan error
	bridge        io.Closer
}

func (p *Process) Start(ctx context.Context, snapshot profiles.Snapshot) error {
	if p.cmd != nil {
		return fmt.Errorf("trap process is already running")
	}
	p.Local.Configure(snapshot)
	raw, err := json.Marshal(childBootstrap{URL: p.URL, Token: p.Local.Token, Certificate: p.Certificate, Snapshot: snapshot})
	if err != nil {
		return fmt.Errorf("encode child configuration: %w", err)
	}
	cmd := exec.Command(p.Executable, "--worker")
	if p.WorkerImage != "" {
		args, err := p.workerArgs(ctx, snapshot)
		if err != nil {
			return err
		}
		if err := p.removeWorker(ctx); err != nil {
			return err
		}
		cmd = exec.Command("docker", args...)
	}
	hideWindow(cmd)
	if p.WorkerImage == "" {
		isolateChild(cmd)
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "AGENT_") || strings.HasPrefix(strings.ToUpper(key), "TRAP_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("child shutdown pipe: %w", err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	var output io.ReadCloser
	if p.WorkerImage != "" {
		// Own this pipe so Cmd.Wait cannot discard a final telemetry packet.
		read, write, err := os.Pipe()
		if err != nil {
			stdin.Close()
			return fmt.Errorf("worker telemetry pipe: %w", err)
		}
		output = read
		cmd.Stdout = write
		defer write.Close()
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		if output != nil {
			output.Close()
		}
		return fmt.Errorf("launch trap: %w", err)
	}
	p.cmd = cmd
	p.stdin = stdin
	p.done = make(chan error, 1)
	go func() { p.done <- cmd.Wait() }()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	interrupt := context.AfterFunc(startup, func() {
		if err := stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			slog.DebugContext(ctx, "worker startup pipe closed", "error", err)
		}
	})
	defer interrupt()
	if _, err := stdin.Write(append(raw, '\n')); err != nil {
		stopErr := p.Stop()
		return fmt.Errorf("send trap bootstrap: %w", errors.Join(err, stopErr))
	}
	if output != nil {
		if err := p.startBridge(ctx, output, stdin); err != nil {
			stopErr := p.Stop()
			return fmt.Errorf("connect worker telemetry: %w", errors.Join(err, stopErr))
		}
	}
	select {
	case <-p.Local.Ready:
		return nil
	case err := <-p.done:
		closeErr := p.closeStreams()
		p.cmd = nil
		if err == nil {
			err = errors.New("worker exited before readiness")
		}
		return fmt.Errorf("trap failed before ready: %w", errors.Join(err, closeErr))
	case <-startup.Done():
		return fmt.Errorf("trap startup interrupted: %w", errors.Join(startup.Err(), p.Stop()))
	}
}

func (p *Process) Stop() (err error) {
	if p.cmd == nil {
		return nil
	}
	// EOF requests graceful shutdown: the child closes TCP sessions, commits their
	// final events through the local socket, then exits.
	var closeErr error
	if p.WorkerImage != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, closeErr = exec.CommandContext(ctx, "docker", "kill", "--signal=TERM", p.workerName()).CombinedOutput()
		cancel()
	} else {
		closeErr = p.stdin.Close()
	}
	defer func() { err = errors.Join(err, p.closeStreams()) }()
	select {
	case err := <-p.done:
		p.cmd = nil
		if err != nil {
			return fmt.Errorf("trap exited unsuccessfully: %w", err)
		}
		return closeErr
	case <-time.After(7 * time.Second):
		if p.WorkerImage != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			removeErr := p.removeWorker(ctx)
			cancel()
			if removeErr != nil {
				closeErr = errors.Join(closeErr, removeErr)
			}
		}
		err := p.cmd.Process.Kill()
		if errors.Is(err, os.ErrProcessDone) {
			err = nil
		}
		<-p.done
		p.cmd = nil
		if err != nil {
			return fmt.Errorf("terminate unresponsive trap: %w", err)
		}
		return fmt.Errorf("trap shutdown timed out: %w", errors.Join(closeErr, errors.New("worker did not stop")))
	}
}

func (p *Process) Poll() error {
	if p.cmd == nil {
		return nil
	}
	select {
	case err := <-p.done:
		closeErr := p.closeStreams()
		p.cmd = nil
		if err == nil {
			return errors.Join(fmt.Errorf("trap process exited unexpectedly"), closeErr)
		}
		return errors.Join(err, closeErr)
	default:
		return nil
	}
}

func (p *Process) closeStreams() error {
	var errs []error
	for _, stream := range []io.Closer{p.stdin, p.bridge} {
		if stream == nil {
			continue
		}
		if err := stream.Close(); err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	p.bridge = nil
	return errors.Join(errs...)
}
