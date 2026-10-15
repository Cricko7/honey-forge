package enginepipe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type Runtime struct {
	writeMu sync.Mutex // serializes acknowledgements and graceful stop requests
	cmd     *exec.Cmd
	input   io.WriteCloser
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex // protects terminal error
	err     error
}

// Start supervises an isolated engine. Dedicated output reserves FD 3 for
// observations when native code may write arbitrary bytes to stdout.
func Start(ctx context.Context, snapshot profiles.Snapshot, c Config, host string, sink func(context.Context, events.AgentEvent) error, cmd *exec.Cmd, dedicated bool) (*Runtime, error) {
	if sink == nil {
		return nil, fmt.Errorf("engine telemetry sink is required")
	}
	if os.Getenv("NM_STDIO") != "1" {
		return nil, fmt.Errorf("engine requires an isolated worker with pipe telemetry")
	}
	if host == "" {
		host = "0.0.0.0"
	}
	cmd.Env = Environment(os.Environ(), snapshot.TypeID)
	cmd.Dir = os.TempDir()
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("engine input pipe: %w", err)
	}
	var output io.ReadCloser
	var writer *os.File
	if dedicated {
		var reader *os.File
		reader, writer, err = os.Pipe()
		output = reader
		cmd.Stdout = io.Discard
		cmd.ExtraFiles = []*os.File{writer}
	} else {
		output, err = cmd.StdoutPipe()
	}
	if err != nil {
		return nil, errors.Join(err, input.Close())
	}

	if err := cmd.Start(); err != nil {
		var closeWriterErr error
		if writer != nil {
			closeWriterErr = writer.Close()
		}
		return nil, errors.Join(fmt.Errorf("start engine: %w", err), input.Close(), output.Close(), closeWriterErr)
	}
	r := &Runtime{cmd: cmd, input: input, done: make(chan struct{})}
	var writerErr error
	if writer != nil {
		writerErr = writer.Close()
	}
	ready := make(chan struct{}, 1)
	go func() {
		b := bridge{snapshot: snapshot, config: c, sessions: map[string]*session{}}
		readErr := r.read(ctx, output, &b, sink, ready)
		var killErr error
		if readErr != nil {
			killErr = cmd.Process.Kill()
			if errors.Is(killErr, os.ErrProcessDone) {
				killErr = nil
			}
		}
		waitErr := cmd.Wait()
		r.mu.Lock()
		r.err = errors.Join(readErr, killErr, waitErr, writerErr, closeOutput(output))
		r.mu.Unlock()
		close(r.done)
	}()
	if err := json.NewEncoder(input).Encode(map[string]any{"config": c, "host": host}); err != nil {
		r.Stop()
		return nil, fmt.Errorf("send engine configuration: %w", err)
	}
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
		go func() {
			select {
			case <-ctx.Done():
				r.Stop()
			case <-r.done:
			}
		}()
		return r, nil
	case <-r.done:
		return nil, fmt.Errorf("engine startup failed: %w", r.Err())
	case <-ctx.Done():
		r.Stop()
		return nil, ctx.Err()
	case <-timer.C:
		r.Stop()
		return nil, fmt.Errorf("engine readiness timeout")
	}
}

func (r *Runtime) Stop() {
	r.once.Do(func() {
		var stopErr error
		select {
		case <-r.done:
		default:
			// Keep stdin open so final session events can still receive ACKs.
			stopErr = r.command("stop\n")
		}
		select {
		case <-r.done:
		case <-time.After(6 * time.Second):
			killErr := r.cmd.Process.Kill()
			if errors.Is(killErr, os.ErrProcessDone) {
				killErr = nil
			}
			<-r.done
			r.mu.Lock()
			r.err = errors.Join(r.err, killErr, fmt.Errorf("engine shutdown timed out"))
			r.mu.Unlock()
		}
		closeErr := r.input.Close()
		if errors.Is(closeErr, os.ErrClosed) {
			closeErr = nil
		}
		if stopErr != nil || closeErr != nil {
			r.mu.Lock()
			r.err = errors.Join(r.err, stopErr, closeErr)
			r.mu.Unlock()
		}
	})
}

func (r *Runtime) command(value string) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	_, err := io.WriteString(r.input, value)
	return err
}

func (r *Runtime) Err() error {
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.err != nil {
			return r.err
		}
		return fmt.Errorf("engine process exited")
	default:
		return nil
	}
}

// Done closes after the engine exits and its observations have been read.
func (r *Runtime) Done() <-chan struct{} { return r.done }
