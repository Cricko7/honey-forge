package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"honey-forge/modules/profiles"
)

// Process uses this executable's private child mode. Configuration and the
// short-lived loopback credential never appear in command-line arguments.
type Process struct {
	Executable string
	URL        string
	Local      *Local
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	done       chan error
}

func (p *Process) Start(ctx context.Context, snapshot profiles.Snapshot) error {
	if p.cmd != nil {
		return fmt.Errorf("trap process is already running")
	}
	p.Local.Configure(snapshot)
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode child configuration: %w", err)
	}
	cmd := exec.Command(p.Executable, "--tcp-trap")
	hideWindow(cmd)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "AGENT_") || strings.HasPrefix(strings.ToUpper(key), "TRAP_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "TRAP_AGENT_URL="+p.URL, "TRAP_AGENT_TOKEN="+p.Local.Token, "TRAP_SNAPSHOT="+string(raw))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("child shutdown pipe: %w", err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("launch trap: %w", err)
	}
	p.cmd = cmd
	p.stdin = stdin
	p.done = make(chan error, 1)
	go func() { p.done <- cmd.Wait() }()
	select {
	case <-p.Local.Ready:
		return nil
	case err := <-p.done:
		p.stdin.Close()
		p.cmd = nil
		return fmt.Errorf("trap failed before ready: %w", err)
	case <-ctx.Done():
		p.Stop()
		return ctx.Err()
	case <-time.After(10 * time.Second):
		p.Stop()
		return fmt.Errorf("trap startup timed out")
	}
}

func (p *Process) Stop() error {
	if p.cmd == nil {
		return nil
	}
	// EOF requests graceful shutdown: the child closes TCP sessions, commits their
	// final events through the local socket, then exits.
	closeErr := p.stdin.Close()
	select {
	case err := <-p.done:
		p.cmd = nil
		if err != nil {
			return fmt.Errorf("trap exited unsuccessfully: %w", err)
		}
		return closeErr
	case <-time.After(7 * time.Second):
		err := p.cmd.Process.Kill()
		<-p.done
		p.cmd = nil
		if err != nil {
			return fmt.Errorf("terminate unresponsive trap: %w", err)
		}
		return fmt.Errorf("trap shutdown timed out")
	}
}

func (p *Process) Poll() error {
	if p.cmd == nil {
		return nil
	}
	select {
	case err := <-p.done:
		p.stdin.Close()
		p.cmd = nil
		if err == nil {
			return fmt.Errorf("trap process exited unexpectedly")
		}
		return err
	default:
		return nil
	}
}
