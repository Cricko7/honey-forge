package sshtrap

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

// execPipe adapts Docker's stdin/stdout transport for the SSH library.
type execPipe struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	out   io.ReadCloser
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex // protects the deadline timer
	timer *time.Timer
}

func startPipe(ctx context.Context, args ...string) (*execPipe, error) {
	return startCommandPipe(exec.CommandContext(ctx, "docker", args...))
}

func startCommandPipe(cmd *exec.Cmd) (*execPipe, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	// Own the read end: Cmd.Wait closes StdoutPipe before a concurrent SSH
	// reader has necessarily drained the final buffered packet.
	out, childOut, err := os.Pipe()
	if err != nil {
		in.Close()
		return nil, err
	}

	cmd.Stdout = childOut
	// SSH backend diagnostics belong to the trusted gateway's application log.
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		childOut.Close()

		return nil, err
	}

	childOut.Close() // only the child keeps a writer, so its exit produces EOF
	p := &execPipe{cmd: cmd, in: in, out: out, done: make(chan struct{})}
	go func() {
		cmd.Wait()
		close(p.done)
	}()

	return p, nil
}

func (p *execPipe) Read(b []byte) (int, error) {
	return p.out.Read(b)
}

func (p *execPipe) Write(b []byte) (int, error) {
	return p.in.Write(b)
}

func (p *execPipe) Close() error {
	var closeErr error
	p.once.Do(func() {
		p.SetDeadline(time.Time{})
		inErr := p.in.Close()
		outErr := p.out.Close()
		killErr := p.cmd.Process.Kill()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}

		<-p.done
		closeErr = errors.Join(inErr, outErr, killErr)
	})

	return closeErr
}

type pipeAddr string

func (a pipeAddr) Network() string {
	return "docker-exec"
}

func (a pipeAddr) String() string {
	return string(a)
}

func (p *execPipe) LocalAddr() net.Addr {
	return pipeAddr("gateway")
}

func (p *execPipe) RemoteAddr() net.Addr {
	return pipeAddr("sandbox")
}

func (p *execPipe) SetDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}

	if !t.IsZero() {
		p.timer = time.AfterFunc(time.Until(t), func() {
			p.Close()
		})
	}

	return nil
}

func (p *execPipe) SetReadDeadline(t time.Time) error {
	return p.SetDeadline(t)
}

func (p *execPipe) SetWriteDeadline(t time.Time) error {
	return p.SetDeadline(t)
}
