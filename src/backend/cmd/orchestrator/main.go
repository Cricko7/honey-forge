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

	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/orchestrator"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("deployment orchestrator stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	wsURL := os.Getenv("AGENT_WS_URL")
	redisURL := os.Getenv("AGENT_REDIS_URL")
	docker := orchestrator.DockerCLI{
		Network: os.Getenv("DECOY_NETWORK"), WSURL: wsURL, RedisURL: redisURL,
		CAFile: os.Getenv("AGENT_CA_FILE"), TCPImage: os.Getenv("DECOY_TCP_IMAGE"), RedisImage: os.Getenv("DECOY_REDIS_IMAGE"),
	}
	if databaseURL == "" || docker.Network == "" || docker.TCPImage == "" || docker.RedisImage == "" {
		return fmt.Errorf("DATABASE_URL, DECOY_NETWORK, DECOY_TCP_IMAGE and DECOY_REDIS_IMAGE are required")
	}
	endpoint, err := url.Parse(wsURL)
	if err != nil || endpoint.Scheme != "wss" || endpoint.Host == "" || endpoint.Path != "/assets/stream" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("AGENT_WS_URL must be wss://host/assets/stream")
	}
	buffer, err := url.Parse(redisURL)
	if err != nil || (buffer.Scheme != "redis" && buffer.Scheme != "rediss") || buffer.Host == "" {
		return fmt.Errorf("AGENT_REDIS_URL must be a redis:// or rediss:// URL")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open orchestration database: %w", err)
	}
	defer pool.Close()
	store := orchestrator.PGStore{Pool: pool, WSURL: wsURL}
	controller := orchestrator.Controller{Store: store, Docker: docker}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		cycle, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := docker.Prepare(cycle)
		if err == nil {
			err = orchestrator.ReconcileLocked(cycle, pool, controller)
		}
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "deployment reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
