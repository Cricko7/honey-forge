package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"honey-forge/internal/agent"
	"honey-forge/internal/decoys"
	"honey-forge/internal/platform/secrets"
	"honey-forge/internal/platform/tlsconfig"
)

func main() {
	logOutput := os.Stdout
	if os.Getenv("TRAP_STDIO") == "1" {
		logOutput = os.Stderr
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(logOutput, nil)))
	if err := run(); err != nil {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) == 2 && os.Args[1] == "--worker" {
		return agent.RunTrap(ctx)
	}
	config, err := tlsconfig.AgentClient(os.Getenv("AGENT_CA_FILE"), os.Getenv("AGENT_CERT_FILE"), os.Getenv("AGENT_KEY_FILE"), os.Getenv("AGENT_SERVER_PINS_FILE"))
	if err != nil {
		return fmt.Errorf("configure agent TLS: %w", err)
	}
	redisCA := os.Getenv("AGENT_REDIS_CA_FILE")
	if redisCA == "" {
		redisCA = os.Getenv("AGENT_CA_FILE")
	}
	redisTLS, err := tlsconfig.Client(redisCA)
	if err != nil {
		return fmt.Errorf("configure Redis TLS: %w", err)
	}
	token, err := secrets.Environment("AGENT_TOKEN")
	if err != nil {
		return err
	}
	redisURL, err := secrets.Environment("AGENT_REDIS_URL")
	if err != nil {
		return err
	}
	if redisURL == "" {
		return fmt.Errorf("AGENT_REDIS_URL is required")
	}
	supported, err := decoys.ForType(os.Getenv("DECOY_TYPE"))
	if err != nil {
		return err
	}
	return agent.Run(ctx, agent.Options{URL: os.Getenv("AGENT_WS_URL"), Token: token, TrapID: os.Getenv("AGENT_TRAP_ID"), RedisURL: redisURL, TLS: config, RedisTLS: redisTLS, SupportedTypes: supported,
		WorkerImage: os.Getenv("TRAP_WORKER_IMAGE"), WorkerNetwork: os.Getenv("TRAP_WORKER_NETWORK"), WorkerBindIP: os.Getenv("DECOY_BIND_IP")})
}
