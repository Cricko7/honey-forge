package postgres

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	config, err := parseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, &connectionError{message: "invalid database configuration", cause: err}
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, &connectionError{message: "database connection failed", cause: err}
	}
	return pool, nil
}

func parseConfig(dsn string) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, &connectionError{message: "invalid database configuration", cause: err}
	}
	secure := func(config *tls.Config) bool {
		return config != nil && !config.InsecureSkipVerify && config.ServerName != ""
	}
	if !secure(config.ConnConfig.TLSConfig) {
		return nil, fmt.Errorf("DATABASE_URL requires sslmode=verify-full")
	}
	config.ConnConfig.TLSConfig.MinVersion = max(config.ConnConfig.TLSConfig.MinVersion, tls.VersionTLS12)
	for _, fallback := range config.ConnConfig.Fallbacks {
		if !secure(fallback.TLSConfig) {
			return nil, fmt.Errorf("DATABASE_URL cannot fall back to unverified or plaintext connections")
		}
		fallback.TLSConfig.MinVersion = max(fallback.TLSConfig.MinVersion, tls.VersionTLS12)
	}
	return config, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, files fs.FS) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("configure migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker), goose.WithAllowOutofOrder(true))
	if err != nil {
		return fmt.Errorf("configure migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
