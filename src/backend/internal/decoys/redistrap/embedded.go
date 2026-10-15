package redistrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"honey-forge/internal/contract"
)

// Embedded owns the same seeded Redis process as the TCP trap, over a private socket.
type Embedded struct {
	process  *redisProcess
	password string
}

func StartEmbedded(ctx context.Context) (*Embedded, error) {
	password := string(contract.NewID())
	process, err := startRedis(ctx, password)
	if err != nil {
		return nil, err
	}

	return &Embedded{process: process, password: password}, nil
}

func (e *Embedded) Stop() { e.process.stop() }

func (e *Embedded) Execute(ctx context.Context, commands [][]string) ([]any, error) {
	if len(commands) < 1 || len(commands) > 32 {
		return nil, fmt.Errorf("expected 1-32 Redis commands")
	}

	for _, args := range commands {
		if len(args) < 1 || len(args) > 64 {
			return nil, fmt.Errorf("invalid Redis command")
		}
	}

	if err := e.process.failure(); err != nil {
		return nil, err
	}

	client := redis.NewClient(&redis.Options{Protocol: 2, Network: "unix", Addr: e.process.socket, Password: e.password, MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, ContextTimeoutEnabled: true})
	defer client.Close()

	results := make([]any, 0, len(commands))
	for _, command := range commands {
		args := make([]any, len(command))
		for i, arg := range command {
			args[i] = arg
		}
		value, err := client.Do(ctx, args...).Result()
		if errors.Is(err, redis.Nil) {
			value = nil
		} else if err != nil {
			var reply redis.Error
			if !errors.As(err, &reply) {
				return nil, fmt.Errorf("execute embedded Redis: %w", err)
			}

			value = map[string]any{"error": err.Error()}
		}

		results = append(results, value)
	}

	return results, nil
}
