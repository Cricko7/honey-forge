package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	commonapp "honey-forge/internal/app"
)

func Run(ctx context.Context, config Config, logger *slog.Logger) error {
	runtime, err := commonapp.Open(ctx, commonapp.Config{DatabaseURL: config.DatabaseURL, CursorKey: config.CursorKey, BrowserOrigins: []string{config.AllowedOrigin}, Logger: logger})
	if err != nil {
		return fmt.Errorf("initialize backend: %w", err)
	}
	defer runtime.Close()
	router := runtime.Router
	server := &http.Server{
		Addr:              config.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	listener, err := net.Listen("tcp", config.HTTPAddr)
	if err != nil {
		return fmt.Errorf("binding HTTP listener: %w", err)
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	logger.Info("HTTP server started", "address", listener.Addr().String())

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving HTTP: %w", err)
		}

	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutting down HTTP: %w", errors.Join(err, server.Close()))
		}

		if err := <-serverErr; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("stopping HTTP listener: %w", err)
		}

		logger.Info("HTTP server stopped")
	}

	return nil
}
