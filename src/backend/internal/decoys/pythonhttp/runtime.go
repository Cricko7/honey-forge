// Package pythonhttp adapts the Python HTTP runtime to the existing trap agent.
package pythonhttp

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"honey-forge/internal/configschema"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/internal/platform/traffic"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

//go:embed scripts/engine.py
var engine string

//go:embed scripts/server.py
var server string

//go:embed scripts/pipeline_server.py
var pipelineServer string

type Runtime struct {
	writeMu sync.Mutex // serializes acknowledgements and incoming pipeline deliveries
	cmd     *exec.Cmd
	input   io.WriteCloser
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex // protects the terminal error
	err     error
}

func Ports(ctx context.Context, snapshot profiles.Snapshot) ([]int, error) {
	if snapshot.TypeID != "custom-http" || snapshot.TypeVersion != 1 || snapshot.ProfileRevision < 1 {
		return nil, fmt.Errorf("unsupported HTTP snapshot")
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return nil, fmt.Errorf("encode HTTP config: %w", err)
	}
	for _, definition := range catalog.BuiltinDefinitions() {
		if definition.Entry.TypeID != "custom-http" {
			continue
		}
		schema, err := configschema.Compile(definition.Entry.ConfigSchema)
		if err != nil {
			return nil, fmt.Errorf("compile HTTP schema: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := schema.Validate(raw, ""); err != nil {
			return nil, fmt.Errorf("validate HTTP config: %w", err)
		}
		if err := catalog.CheckCustomHTTPConfig(raw); err != nil {
			return nil, err
		}
		var config struct {
			Services []struct {
				Port int `json:"port"`
			} `json:"services"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode HTTP services: %w", err)
		}
		ports := make([]int, 0, len(config.Services))
		for _, service := range config.Services {
			ports = append(ports, service.Port)
		}
		return ports, nil
	}
	return nil, fmt.Errorf("HTTP descriptor missing")
}

func Start(ctx context.Context, snapshot profiles.Snapshot, host string, sink func(context.Context, events.AgentEvent) error) (*Runtime, error) {
	if _, err := Ports(ctx, snapshot); err != nil {
		return nil, err
	}
	if sink == nil {
		return nil, fmt.Errorf("telemetry sink is required")
	}
	if host == "" {
		host = "0.0.0.0"
	}
	executable := os.Getenv("HF_PYTHON")
	if executable == "" {
		executable = "python3"
	}
	cmd := exec.Command(executable, "-I", "-u", "-c", engine+"\n"+pipelineServer+"\n"+server)
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("Python input pipe: %w", err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Join(err, input.Close())
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.Join(fmt.Errorf("start Python HTTP: %w", err), input.Close(), output.Close())
	}
	r := &Runtime{cmd: cmd, input: input, done: make(chan struct{})}
	ready := make(chan struct{}, 1)
	go func() {
		err := r.read(ctx, output, sink, ready)
		if err != nil {
			_ = cmd.Process.Kill()
		}
		waitErr := cmd.Wait()
		r.mu.Lock()
		r.err = errors.Join(err, waitErr)
		r.mu.Unlock()
		close(r.done)
	}()
	if err := json.NewEncoder(input).Encode(map[string]any{"snapshot": snapshot, "host": host}); err != nil {
		r.Stop()
		return nil, fmt.Errorf("send Python config: %w", err)
	}
	timer := time.NewTimer(10 * time.Second)
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
		return nil, fmt.Errorf("Python HTTP startup failed: %w", r.Err())
	case <-ctx.Done():
		r.Stop()
		return nil, ctx.Err()
	case <-timer.C:
		r.Stop()
		return nil, fmt.Errorf("Python HTTP readiness timeout")
	}
}

func (r *Runtime) read(ctx context.Context, output io.Reader, sink func(context.Context, events.AgentEvent) error, ready chan<- struct{}) error {
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 16384), 32768)
	initialized := false
	for scanner.Scan() {
		var message struct {
			Type    string            `json:"type"`
			Event   events.AgentEvent `json:"event"`
			Frame   traffic.Frame     `json:"frame"`
			Request pipeline.Request  `json:"request"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			return fmt.Errorf("decode Python message: %w", err)
		}
		switch message.Type {
		case "ready":
			if initialized {
				return fmt.Errorf("duplicate Python readiness")
			}
			initialized = true
			ready <- struct{}{}
		case "event":
			if !initialized {
				return fmt.Errorf("Python event before readiness")
			}
			eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			err := sink(eventCtx, message.Event)
			cancel()
			writeErr := r.write(map[string]any{"event_id": message.Event.EventID, "ok": err == nil})
			if err != nil || writeErr != nil {
				return fmt.Errorf("Python telemetry: %w", errors.Join(err, writeErr))
			}
		case "traffic":
			if !initialized {
				return fmt.Errorf("Python traffic before readiness")
			}
			if err := traffic.CheckFrame(message.Frame); err != nil {
				return err
			}
			traffic.Mirror(ctx, message.Frame)
		case "pipeline_send":
			if !initialized {
				return fmt.Errorf("pipeline send before readiness")
			}
			// Allow the local four-second admission timeout to reply before cancelling its pipe.
			forwardCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
			err := pipeline.Send(forwardCtx, message.Request)
			cancel()
			id := message.Request.RequestID
			if err != nil {
				id = ""
			}
			if err := r.write(map[string]any{"pipeline_request_id": message.Request.RequestID, "accepted": id != ""}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected Python message")
		}
	}
	return scanner.Err()
}

func (r *Runtime) write(value any) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return json.NewEncoder(r.input).Encode(value)
}

func (r *Runtime) Receive(ctx context.Context, d pipeline.Delivery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := pipeline.Check(d.Request); err != nil {
		return err
	}
	if !time.Now().Before(d.ExpiresAt) {
		return nil
	}
	return r.write(map[string]any{"type": "pipeline_receive", "delivery": d})
}

func (r *Runtime) Stop() {
	r.once.Do(func() {
		_ = r.input.Close()
		select {
		case <-r.done:
		case <-time.After(7 * time.Second):
			_ = r.cmd.Process.Kill()
			<-r.done
		}
	})
}

func (r *Runtime) Err() error {
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.err == nil {
			return fmt.Errorf("Python HTTP process exited")
		}
		return r.err
	default:
		return nil
	}
}
