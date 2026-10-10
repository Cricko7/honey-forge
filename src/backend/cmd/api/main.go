package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"honey-forge/internal/app"
	"honey-forge/modules/events"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(); err != nil {
		logger.Error("API stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cert, key := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	if cert == "" || key == "" {
		return fmt.Errorf("TLS_CERT_FILE and TLS_KEY_FILE are required")
	}
	keyBytes, err := base64.StdEncoding.Strict().DecodeString(os.Getenv("CURSOR_KEY"))
	if err != nil || len(keyBytes) != 32 {
		return fmt.Errorf("CURSOR_KEY must be a base64-encoded 32-byte key")
	}
	origins := strings.Split(os.Getenv("BROWSER_ORIGINS"), ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	if len(origins) == 0 || origins[0] == "" {
		return fmt.Errorf("BROWSER_ORIGINS is required")
	}
	var trustedProxies []string
	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			trustedProxies = append(trustedProxies, p)
		}
	}
	startup, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()
	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}
	topic := os.Getenv("KAFKA_TELEMETRY_TOPIC")
	if topic == "" {
		topic = "honey-forge.telemetry"
	}
	publisher, err := events.NewKafkaPublisher(brokers, topic)
	if err != nil {
		return err
	}
	defer publisher.Close()
	runtime, err := app.Open(startup, app.Config{DatabaseURL: os.Getenv("DATABASE_URL"), CursorKey: keyBytes, BrowserOrigins: origins, AgentWSURL: os.Getenv("AGENT_WS_URL"), EventPublisher: publisher, TrustedProxies: trustedProxies})
	cancelStartup()
	if err != nil {
		return err
	}
	defer runtime.Close()
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8443"
	}
	server := &http.Server{Addr: addr, Handler: runtime.Router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServeTLS(cert, key) }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve API: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			closeErr := server.Close()
			return fmt.Errorf("shutdown API: %w", errors.Join(err, closeErr))
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("stop API: %w", err)
		}
		return nil
	}
}
