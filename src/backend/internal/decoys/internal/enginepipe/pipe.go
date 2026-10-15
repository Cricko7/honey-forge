package enginepipe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"honey-forge/modules/events"
	"io"
	"os"
	"strings"
)

func Environment(parent []string, engine string) []string {
	env := []string{"PYTHONDONTWRITEBYTECODE=1", "HOME=/run/" + engine, "TMPDIR=/run/" + engine}
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "PATH", "SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "TEMP", "TMP":
			env = append(env, entry)
		}
	}
	return env
}

func (r *Runtime) read(ctx context.Context, output io.Reader, b *bridge, sink func(context.Context, events.AgentEvent) error, ready chan<- struct{}) error {
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 16384)
	initialized := false
	for scanner.Scan() {
		var m message
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&m); err != nil {
			return fmt.Errorf("invalid engine pipe message: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("multiple engine pipe values")
		}
		if m.Kind == "ready" {
			if initialized {
				return fmt.Errorf("duplicate engine readiness")
			}
			initialized = true
			ready <- struct{}{}
			continue
		}
		if !initialized {
			return fmt.Errorf("engine observation before readiness")
		}
		if err := b.accept(ctx, m, sink); err != nil {
			return err
		}
		// ACK only after the local client has durably journaled the observation.
		if err := r.command("ok\n"); err != nil {
			return fmt.Errorf("acknowledge engine event: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read engine telemetry: %w", err)
	}
	return nil
}

func closeOutput(output io.Closer) error {
	err := output.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
