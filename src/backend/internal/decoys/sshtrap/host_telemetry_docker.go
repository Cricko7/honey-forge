package sshtrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"honey-forge/internal/contract"
)

var sensorName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// This fixed script contains no request data or interpolation. Closing the
// attached stdin stops ONLY this subscription inside the sensor container;
// killing the local Docker CLI alone would leave a remote tetra process alive.
const sensorStreamScript = `exec 3<&0
tetra getevents --event-types PROCESS_EXEC --include-fields time,process.exec_id,process.pid,process.uid,process.binary,process.cwd,process.docker,parent.pid </dev/null &
stream_pid=$!
(cat <&3 >/dev/null; kill "$stream_pid" 2>/dev/null) &
guard_pid=$!
trap 'kill "$stream_pid" "$guard_pid" 2>/dev/null; wait "$stream_pid" "$guard_pid" 2>/dev/null' EXIT
wait "$stream_pid"`

func hostTelemetrySettings(ctx context.Context) (string, *hostObserver, error) {
	name := os.Getenv("NM_TETRAGON_CONTAINER")
	if !sensorName.MatchString(name) {
		return "", nil, fmt.Errorf("NM_TETRAGON_CONTAINER must name the local operator-installed Tetragon sensor")
	}
	trap := os.Getenv("NM_INSTANCE_ID")
	generation, err := strconv.ParseInt(os.Getenv("NM_DEPLOYMENT_GENERATION"), 10, 64)
	if err != nil || generation < 1 || generation > 9007199254740991 || !contract.ValidID(trap) {
		return "", nil, fmt.Errorf("host telemetry requires a valid NM_INSTANCE_ID and NM_DEPLOYMENT_GENERATION")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := dockerCall(probeCtx, "info", "--format", "{{.ID}}")
	if err != nil {
		return "", nil, fmt.Errorf("read Docker node identity: %w", err)
	}
	host := strings.TrimSpace(string(raw))
	if host == "" || len(host) > 255 {
		return "", nil, fmt.Errorf("invalid Docker node identity")
	}
	return name, newHostObserver(host, trap, generation), nil
}

func (o *hostObserver) inspectSandbox(ctx context.Context, name string, record *recording) error {
	raw, err := dockerCall(ctx, "inspect", "--type", "container", "--format", "{{json .}}", name)
	if err != nil {
		return err
	}
	var inspected struct {
		ID    string `json:"Id"`
		State struct {
			Running   bool
			StartedAt time.Time
		}
		Config struct{ Labels map[string]string }
	}
	if err := json.Unmarshal(raw, &inspected); err != nil {
		return fmt.Errorf("decode inspected sandbox: %w", err)
	}
	labels := inspected.Config.Labels
	if !inspected.State.Running || labels["com.nodemetrics.sandbox"] != "true" || labels["com.nodemetrics.instance"] != o.trap || labels["com.nodemetrics.session"] != record.sessionID || labels["com.nodemetrics.generation"] != strconv.FormatInt(o.generation, 10) {
		return fmt.Errorf("inspected sandbox ownership does not match active session")
	}
	return o.bind(inspected.ID, inspected.State.StartedAt, record)
}

func (o *hostObserver) start(ctx context.Context, name string, fail func(error)) (<-chan struct{}, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := dockerCall(probeCtx, "inspect", "--type", "container", "--format", "{{json .}}", name)
	if err != nil {
		return nil, fmt.Errorf("Tetragon sensor unavailable: %w", err)
	}
	var sensor struct {
		State  struct{ Running bool }
		Config struct{ Image string }
	}
	if err := json.Unmarshal(raw, &sensor); err != nil {
		return nil, fmt.Errorf("decode Tetragon sensor: %w", err)
	}
	if !sensor.State.Running || sensor.Config.Image != "quay.io/cilium/tetragon:v1.7.0" {
		return nil, fmt.Errorf("Tetragon sensor must run pinned quay.io/cilium/tetragon:v1.7.0")
	}

	streamCtx, stop := context.WithCancel(ctx)
	cmd := exec.CommandContext(streamCtx, "docker", "exec", "--interactive", name, "sh", "-c", sensorStreamScript)
	input, err := cmd.StdinPipe()
	if err != nil {
		stop()
		return nil, fmt.Errorf("open sensor lifetime pipe: %w", err)
	}
	cmd.Cancel = input.Close
	cmd.WaitDelay = 2 * time.Second
	reader, writer, err := os.Pipe()
	if err != nil {
		stop()
		return nil, fmt.Errorf("open sensor output pipe: %w", errors.Join(err, input.Close()))
	}
	cmd.Stdout = writer
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		stop()
		return nil, fmt.Errorf("start Tetragon reader: %w", errors.Join(err, input.Close(), reader.Close(), writer.Close()))
	}
	if err := writer.Close(); err != nil {
		stop()
		return nil, fmt.Errorf("close parent sensor output: %w", errors.Join(err, reader.Close(), cmd.Wait()))
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := o.consume(reader)
		stop()
		err = errors.Join(err, reader.Close(), cmd.Wait())
		if ctx.Err() == nil {
			fail(fmt.Errorf("host telemetry unavailable: %w", err))
		}
		slog.Info("host telemetry reader stopped", "accepted", o.accepted.Load(), "unattributed", o.ignored.Load())
	}()
	return done, nil
}
