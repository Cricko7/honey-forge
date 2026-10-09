package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"honey-forge/src/backend/internal/app"
)

func main() {
	config, err := app.LoadConfig()
	if err != nil {
		logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		logger.Error("invalid server configuration", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: config.LogLevel,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, config, logger); err != nil {
		logger.Error("server stopped with an error", "error", err)
		os.Exit(1)
	}
}
