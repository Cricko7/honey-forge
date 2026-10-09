package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	authrepo "github.com/Cricko7/honey-forge/src/backend/modules/auth/repository"
	authservice "github.com/Cricko7/honey-forge/src/backend/modules/auth/service"
	"github.com/Cricko7/honey-forge/src/backend/modules/profiles"
	profilerepo "github.com/Cricko7/honey-forge/src/backend/modules/profiles/repository"
	profileservice "github.com/Cricko7/honey-forge/src/backend/modules/profiles/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Run(ctx context.Context, config Config, logger *slog.Logger) error {
	poolConfig, err := pgxpool.ParseConfig(config.DatabaseURL)
	if err != nil {
		// The driver's parse error can include the DSN and its password.
		return errors.New("DATABASE_URL is invalid")
	}

	poolConfig.MaxConns = 10
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("creating PostgreSQL pool: %w", err)
	}
	defer pool.Close()

	startupCtx, cancelStartup := context.WithTimeout(ctx, 5*time.Second)
	defer cancelStartup()

	if err := pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("connecting to PostgreSQL: %w", err)
	}

	var schemaReady bool
	err = pool.QueryRow(startupCtx, `
		SELECT to_regclass('organizations') IS NOT NULL
			AND to_regclass('users') IS NOT NULL
			AND to_regclass('operator_sessions') IS NOT NULL
			AND to_regclass('auth_audit') IS NOT NULL
			AND to_regclass('auth_changes') IS NOT NULL
			AND to_regclass('profiles') IS NOT NULL
			AND to_regclass('profile_revisions') IS NOT NULL
			AND to_regclass('profile_requests') IS NOT NULL
			AND to_regclass('profile_audit') IS NOT NULL
			AND to_regclass('profile_changes') IS NOT NULL
	`).Scan(&schemaReady)
	if err != nil {
		return fmt.Errorf("checking auth schema: %w", err)
	}
	if !schemaReady {
		return errors.New("backend schema is missing; apply Goose migrations before starting the API")
	}

	repository := authrepo.NewRepository(pool)
	service := authservice.NewService(repository)
	profileService := profileservice.NewService(profilerepo.NewRepository(pool), profiles.Dependencies{
		LookupType: lookupProfileType, HasLiveBindings: profilerepo.CheckLiveBindings,
	})
	router, err := NewRouter(service, logger, config.AllowedOrigin, profileService)
	if err != nil {
		return fmt.Errorf("creating HTTP router: %w", err)
	}

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
