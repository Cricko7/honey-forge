package agent

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
)

type Options struct {
	URL            string
	Token          string
	TrapID         string
	JournalPath    string
	RedisURL       string
	Executable     string
	Hostname       string
	BootID         string
	TLS            *tls.Config
	SupportedTypes []agentws.SupportedType
}

func Run(ctx context.Context, opts Options) error {
	endpoint, err := url.Parse(opts.URL)
	if err != nil || endpoint.Scheme != "wss" || endpoint.Host == "" || endpoint.Path != "/assets/stream" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("AGENT_WS_URL must be wss://host/assets/stream")
	}
	if opts.Token == "" || !contract.ValidID(opts.TrapID) || (opts.JournalPath == "" && opts.RedisURL == "") {
		return fmt.Errorf("AGENT_TOKEN, AGENT_TRAP_ID and AGENT_REDIS_URL are required")
	}
	if opts.Executable == "" {
		opts.Executable, err = os.Executable()
		if err != nil {
			return fmt.Errorf("locate agent executable: %w", err)
		}
	}
	if opts.Hostname == "" {
		opts.Hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("read hostname: %w", err)
		}
	}
	opts.BootID = string(contract.NewID())
	var j *Journal
	if opts.RedisURL != "" {
		j, err = OpenRedisJournal(ctx, opts.RedisURL, opts.TrapID, 64<<20)
	} else {
		j, err = OpenJournal(opts.JournalPath, opts.TrapID, 64<<20)
	}
	if err != nil {
		return err
	}
	defer j.Close()
	if err := j.RecoverSessions(); err != nil {
		return err
	}
	var secret [32]byte
	if _, err := cryptorand.Read(secret[:]); err != nil {
		return fmt.Errorf("generate local credential: %w", err)
	}
	local, err := NewLocal(j, base64.RawURLEncoding.EncodeToString(secret[:]))
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for local trap: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	localCtx, localCancel := context.WithCancel(context.WithoutCancel(ctx))
	defer localCancel()
	server := &http.Server{Handler: local.Handler(localCtx), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	serving := make(chan error, 1)
	go func() { serving <- server.Serve(listener) }()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	p := &Process{Executable: opts.Executable, URL: "ws://" + listener.Addr().String() + "/trap-stream", Local: local}
	s := NewService(j, p)
	defer s.Shutdown()
	if err := s.Restore(ctx); err != nil {
		return err
	}
	storageCtx, storageCancel := context.WithCancel(ctx)
	storageDone := make(chan struct{})
	go func() {
		defer close(storageDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-storageCtx.Done():
				return
			case <-ticker.C:
				if err := j.CheckStorage(storageCtx); err != nil && storageCtx.Err() == nil {
					s.Failed("buffer_unavailable")
				}
			}
		}
	}()
	defer func() { storageCancel(); <-storageDone }()
	delay := time.Second
	for {
		err := session(ctx, opts, j, s, local, p)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, ErrRevoked) {
			return err
		}
		slog.WarnContext(ctx, "agent connection interrupted; queued events retained")
		wait := min(30*time.Second, delay+time.Duration(rand.Int64N(int64(time.Second))))
		select {
		case <-ctx.Done():
			return nil
		case err := <-serving:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return fmt.Errorf("local agent server: %w", err)
		case <-time.After(wait):
		}
		delay = min(30*time.Second, delay*2)
	}
}
