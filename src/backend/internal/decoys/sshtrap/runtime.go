package sshtrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"honey-forge/internal/decoys/camouflage"
	"honey-forge/internal/platform/traffic"
	"honey-forge/modules/profiles"
)

type Runtime struct {
	listener      net.Listener
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	telemetryDone <-chan struct{}
	mu            sync.Mutex // protects the first runtime failure
	failure       error
}

func Start(ctx context.Context, snapshot profiles.Snapshot, host string, sink Sink) (*Runtime, error) {
	var sensor string
	var observer *hostObserver
	if snapshot.TypeVersion == 2 {
		var err error
		sensor, observer, err = hostTelemetrySettings(ctx)
		if err != nil {
			return nil, err
		}
	}
	image := os.Getenv("NM_SANDBOX_IMAGE")
	if image == "" {
		return nil, fmt.Errorf("NM_SANDBOX_IMAGE is required")
	}

	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("SSH gateway requires Docker CLI: %w", err)
	}

	return start(ctx, snapshot, host, sink, dockerBackend{image: image, telemetry: observer, sensor: sensor})
}

func start(ctx context.Context, snapshot profiles.Snapshot, host string, sink Sink, b backend) (*Runtime, error) {
	config, err := ParseConfig(ctx, snapshot)
	if err != nil {
		return nil, err
	}

	if sink == nil || b == nil {
		return nil, fmt.Errorf("SSH backend and event sink are required")
	}

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}

	service := config.Services[0]
	serverConfig := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-OpenSSH_9.2p1",
		MaxAuthTries:  3,
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if meta.User() != service.Username || subtle.ConstantTimeCompare(password, []byte(service.Password)) != 1 {
				return nil, fmt.Errorf("authentication rejected")
			}

			return nil, nil
		},
	}
	serverConfig.AddHostKey(signer)
	listener, err := traffic.Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(service.Port)))
	if err != nil {
		return nil, fmt.Errorf("listen SSH: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	r := &Runtime{listener: listener, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	context.AfterFunc(ctx, func() {
		listener.Close()
	})
	if docker, ok := b.(dockerBackend); ok && docker.telemetry != nil {
		r.telemetryDone, err = docker.telemetry.start(ctx, docker.sensor, r.fail)
		if err != nil {
			cancel()
			listener.Close()
			return nil, err
		}
	}
	slots := make(chan struct{}, config.Session.MaxConnections)
	go func() {
		var sessions sync.WaitGroup
		defer func() {
			sessions.Wait()
			close(r.done)
		}()

		for {
			conn, err := listener.Accept()
			if err != nil {
				if ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
					r.fail(err)
				}

				return
			}

			select {
			case slots <- struct{}{}:
				sessions.Go(func() {
					defer func() {
						<-slots
					}()

					r.serve(conn, serverConfig, snapshot, config, sink, b)
				})
			default:
				conn.Close()
			}
		}
	}()

	return r, nil
}

func (r *Runtime) serve(raw net.Conn, serverConfig *ssh.ServerConfig, snapshot profiles.Snapshot, config Config, sink Sink, b backend) {
	ctx, cancel := context.WithTimeout(r.ctx, time.Duration(config.Session.MaxSeconds)*time.Second)
	defer cancel()

	stop := context.AfterFunc(ctx, func() {
		raw.Close()
	})
	defer stop()
	defer raw.Close()

	if err := raw.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return
	}

	if !camouflage.Response(ctx) {
		return
	}

	// Each connection owns its callback context; shutdown interrupts authentication.
	sessionConfig := *serverConfig
	sessionConfig.PasswordCallback = func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if !camouflage.Authentication(ctx) {
			return nil, ctx.Err()
		}

		return serverConfig.PasswordCallback(meta, password)
	}

	conn, channels, requests, err := ssh.NewServerConn(raw, &sessionConfig)
	if err != nil {
		return
	}

	defer conn.Close()

	if err := raw.SetDeadline(time.Time{}); err != nil {
		return
	}

	service := config.Services[0]
	record := newRecording(ctx, snapshot, service.Name, raw.RemoteAddr().String(), service.Port, config.Session.MaxRecordingBytes, sink)
	record.failure = func(err error) {
		r.recordingFailed(err, cancel)
	}
	started := time.Now()
	if err := record.event("service.connection_opened", map[string]any{}); err != nil {
		r.fail(err)
		return
	}

	defer func() {
		reason := "peer_closed"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "session_timeout"
		} else if r.ctx.Err() != nil {
			reason = "service_stopped"
		}

		if observed := record.closeReason(); observed != "" {
			reason = observed
		}

		if err := record.event("service.connection_closed", map[string]any{"duration_ms": time.Since(started).Milliseconds(), "bytes_received": record.receivedBytes(), "reason": reason}); err != nil {
			r.fail(err)
		}
	}()

	client, cleanup, err := b.Open(ctx, record, config.Session.MaxSeconds)
	if err != nil {
		r.fail(fmt.Errorf("open SSH sandbox: %w", err))
		return
	}

	defer func() {
		if err := cleanup(); err != nil {
			r.fail(fmt.Errorf("remove SSH sandbox: %w", err))
		}
	}()
	defer client.Close()

	go ssh.DiscardRequests(requests) // exits when conn is closed
	var workers sync.WaitGroup
	channelSlots := make(chan struct{}, 8)
	for incoming := range channels {
		if incoming.ChannelType() != "session" {
			incoming.Reject(ssh.Prohibited, "forwarding is disabled")
			continue
		}

		select {
		case channelSlots <- struct{}{}:
		default:
			incoming.Reject(ssh.ResourceShortage, "channel limit")
			continue
		}

		channel, reqs, err := incoming.Accept()
		if err != nil {
			<-channelSlots
			break
		}

		workers.Go(func() {
			defer func() {
				<-channelSlots
			}()

			r.channel(ctx, channel, reqs, client, record, config, cancel)
		})
	}

	cancel() // closes pending channels and backend reads before joining workers
	client.Close()
	workers.Wait()
}

func (r *Runtime) fail(err error) {
	r.mu.Lock()
	if r.failure == nil {
		r.failure = err
	}

	r.mu.Unlock()
	r.cancel()
}

func (r *Runtime) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.failure
}

func (r *Runtime) Stop() {
	r.cancel()
	<-r.done
	if r.telemetryDone != nil {
		<-r.telemetryDone
	}
}
