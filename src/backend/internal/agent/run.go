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
	"honey-forge/internal/platform/tlsconfig"
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
	RedisTLS       *tls.Config
	SupportedTypes []agentws.SupportedType
	WorkerImage    string
	WorkerNetwork  string
	WorkerBindIP   string
}

func Run(ctx context.Context, opts Options) error {
	if (opts.WorkerImage == "") != (opts.WorkerNetwork == "") || opts.WorkerNetwork == "host" || opts.WorkerNetwork == "none" {
		return fmt.Errorf("TRAP_WORKER_IMAGE and an isolated TRAP_WORKER_NETWORK must be configured together")
	}
	endpoint, err := url.Parse(opts.URL)
	if err != nil || endpoint.Scheme != "wss" || endpoint.Host == "" || endpoint.Path != "/assets/stream" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("AGENT_WS_URL must be wss://host/assets/stream")
	}
	if opts.Token == "" || !contract.ValidID(opts.TrapID) || (opts.JournalPath == "" && opts.RedisURL == "") {
		return fmt.Errorf("AGENT_TOKEN, AGENT_TRAP_ID and AGENT_REDIS_URL are required")
	}
	opts.TLS, err = tlsconfig.Secure(opts.TLS)
	if err != nil {
		return fmt.Errorf("configure backend TLS: %w", err)
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
		j, err = OpenRedisJournal(ctx, opts.RedisURL, opts.TrapID, 64<<20, opts.RedisTLS)
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
	localTLS, localCertificate, err := newLocalTLS()
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
	server := &http.Server{Handler: local.Handler(localCtx), TLSConfig: localTLS, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	serving := make(chan error, 1)
	go func() { serving <- server.ServeTLS(listener, "", "") }()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	p := &Process{Executable: opts.Executable, URL: "wss://" + listener.Addr().String() + "/trap-stream", Certificate: localCertificate, Local: local,
		WorkerImage: opts.WorkerImage, WorkerNetwork: opts.WorkerNetwork, WorkerBindIP: opts.WorkerBindIP, TrapID: opts.TrapID}
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
