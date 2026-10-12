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
	"honey-forge/internal/orchestrator"
	"honey-forge/internal/platform/secrets"
	"honey-forge/internal/platform/tlsconfig"
)

func main() {
	logOutput := os.Stdout
	if os.Getenv("NM_STDIO") == "1" {
		logOutput = os.Stderr
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(logOutput, nil)))
	if err := run(); err != nil {
		slog.Error("node-metrics stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if len(os.Args) == 2 && os.Args[1] == "--worker" {
		return agent.RunTrap(ctx)
	}

	config, err := tlsconfig.AgentClient(os.Getenv("NM_CA_FILE"), os.Getenv("NM_CERT_FILE"), os.Getenv("NM_KEY_FILE"), os.Getenv("NM_SERVER_PINS_FILE"))
	if err != nil {
		return fmt.Errorf("configure node TLS: %w", err)
	}

	redisCA := os.Getenv("NM_STORE_CA_FILE")
	if redisCA == "" {
		redisCA = os.Getenv("NM_CA_FILE")
	}

	redisTLS, err := tlsconfig.Client(redisCA)
	if err != nil {
		return fmt.Errorf("configure store TLS: %w", err)
	}

	token, err := secrets.Environment("NM_TOKEN")
	if err != nil {
		return err
	}

	redisURL, err := secrets.Environment("NM_STORE_URL")
	if err != nil {
		return err
	}

	if redisURL == "" {
		return fmt.Errorf("NM_STORE_URL is required")
	}

	supported, err := decoys.ForType(os.Getenv("NM_ROLE"))
	if err != nil {
		return err
	}

	if os.Getenv("NM_SANDBOX_IMAGE") != "" {
		cleanupCtx, stopCleanup := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)

			sandboxCleanup(cleanupCtx, (orchestrator.DockerCLI{}).CleanupSandboxes)
		}()

		defer func() {
			stopCleanup()
			<-done
		}()
	}

	return agent.Run(ctx, agent.Options{
		URL:            os.Getenv("NM_WS_URL"),
		Token:          token,
		TrapID:         os.Getenv("NM_INSTANCE_ID"),
		RedisURL:       redisURL,
		TLS:            config,
		RedisTLS:       redisTLS,
		SupportedTypes: supported,
		WorkerImage:    os.Getenv("NM_WORKER_IMAGE"),
		WorkerNetwork:  os.Getenv("NM_WORKER_NETWORK"),
		WorkerBindIP:   os.Getenv("NM_BIND_IP"),
	})
}
