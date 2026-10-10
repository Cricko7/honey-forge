package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"honey-forge/internal/agent"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
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
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if path := os.Getenv("AGENT_CA_FILE"); path != "" {
		pem, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read agent CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return fmt.Errorf("AGENT_CA_FILE has no certificates")
		}
		config.RootCAs = roots
	}
	if os.Getenv("AGENT_REDIS_URL") == "" {
		return fmt.Errorf("AGENT_REDIS_URL is required")
	}
	return agent.Run(ctx, agent.Options{URL: os.Getenv("AGENT_WS_URL"), Token: os.Getenv("AGENT_TOKEN"), TrapID: os.Getenv("AGENT_TRAP_ID"), RedisURL: os.Getenv("AGENT_REDIS_URL"), TLS: config})
}
