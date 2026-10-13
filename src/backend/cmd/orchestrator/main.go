package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"honey-forge/internal/orchestrator"
	"honey-forge/internal/platform/secrets"
	"honey-forge/internal/postgres"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("deployment orchestrator stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL, err := postgres.EnvironmentURL()
	if err != nil {
		return err
	}

	wsURL := os.Getenv("AGENT_WS_URL")
	redisURL, err := secrets.Environment("AGENT_REDIS_URL")
	if err != nil {
		return err
	}

	docker := orchestrator.DockerCLI{
		ClientCAFile:      os.Getenv("AGENT_CLIENT_CA_FILE"),
		ClientCAKeyFile:   os.Getenv("AGENT_CLIENT_CA_KEY_FILE"),
		ServerPinsFile:    os.Getenv("AGENT_SERVER_PINS_FILE"),
		ProxyService:      os.Getenv("AGENT_PROXY_SERVICE"),
		ManagementNetwork: os.Getenv("MGMT_NETWORK"),
		WorkerBindIP:      os.Getenv("DECOY_BIND_IP"),
		Network:           os.Getenv("DECOY_NETWORK"),
		WSURL:             wsURL,
		WSHostIP:          os.Getenv("AGENT_WS_HOST_IP"),
		RedisURL:          redisURL,
		CAFile:            os.Getenv("AGENT_CA_FILE"),
		RedisCAFile:       os.Getenv("AGENT_REDIS_CA_FILE"),
		TCPImage:          os.Getenv("DECOY_TCP_IMAGE"),
		RedisImage:        os.Getenv("DECOY_REDIS_IMAGE"),
		HoneytokenImage:   os.Getenv("DECOY_HONEYTOKEN_IMAGE"),
		SSHImage:          os.Getenv("DECOY_SSH_IMAGE"),
		SandboxImage:      os.Getenv("SSH_SANDBOX_IMAGE"),
		TetragonContainer: os.Getenv("TETRAGON_CONTAINER"),
	}
	if docker.ClientCAFile == "" || docker.ClientCAKeyFile == "" || docker.ServerPinsFile == "" {
		return fmt.Errorf("AGENT_CLIENT_CA_FILE, AGENT_CLIENT_CA_KEY_FILE and AGENT_SERVER_PINS_FILE are required")
	}

	if databaseURL == "" || docker.Network == "" || docker.TCPImage == "" || docker.RedisImage == "" {
		return fmt.Errorf("DATABASE_URL, DECOY_NETWORK, DECOY_TCP_IMAGE and DECOY_REDIS_IMAGE are required")
	}

	endpoint, err := url.Parse(wsURL)
	if err != nil || endpoint.Scheme != "wss" || endpoint.Host == "" || endpoint.Path != "/assets/stream" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("AGENT_WS_URL must be wss://host/assets/stream")
	}

	buffer, err := url.Parse(redisURL)
	if err != nil || buffer.Scheme != "rediss" || buffer.Host == "" {
		return fmt.Errorf("AGENT_REDIS_URL must be a rediss:// URL")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open orchestration database: %w", err)
	}

	defer pool.Close()

	store := orchestrator.PGStore{Pool: pool, WSURL: wsURL}
	controller := orchestrator.Controller{Store: store, Docker: docker}

	return runLoop(ctx, docker.Prepare, func(cycle context.Context) error {
		if err := store.SyncNodes(cycle, docker); err != nil {
			return err
		}

		return orchestrator.ReconcileLocked(cycle, pool, controller)
	}, func(cycle context.Context) error {
		return docker.CleanupSandboxes(cycle, time.Now())
	})
}
