package redistrap

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisProcess struct {
	socket string
	cancel context.CancelFunc
	done   chan struct{}
	err    error // published by done
}

func startRedis(ctx context.Context, password string) (*redisProcess, error) {
	base := os.TempDir()
	if _, err := os.Stat("/run/redis"); err == nil {
		base = "/run/redis"
	}

	dir, err := os.MkdirTemp(base, "redis-trap-")
	if err != nil {
		return nil, fmt.Errorf("create private Redis directory: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	p := &redisProcess{socket: filepath.Join(dir, "redis.sock"), cancel: cancel, done: make(chan struct{})}
	// Unix sockets preserve the worker's replies-only network firewall. Hashing
	// the bait password avoids config injection and putting credentials in argv.
	config := fmt.Sprintf("port 0\nunixsocket %s\nunixsocketperm 700\nsave \"\"\nappendonly no\nmaxmemory 32mb\nmaxmemory-policy noeviction\nmaxclients 128\nloglevel warning\nlogfile \"\"\nuser default on #%x ~* &* +@all\n", p.socket, sha256.Sum256([]byte(password)))
	cmd := exec.CommandContext(ctx, "redis-server", "-")
	cmd.Stdin = strings.NewReader(config)
	// stdout belongs exclusively to the worker's TLS telemetry pipe.
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, errors.Join(fmt.Errorf("start redis-server: %w", err), os.RemoveAll(dir))
	}

	go func() {
		err := cmd.Wait()
		if ctx.Err() == nil {
			p.err = fmt.Errorf("redis-server exited unexpectedly: %w", errors.Join(err, errors.New("Redis is unavailable")))
		}

		p.err = errors.Join(p.err, os.RemoveAll(dir))
		close(p.done)
	}()

	client := redis.NewClient(&redis.Options{Network: "unix", Addr: p.socket, Password: password, MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	defer client.Close()

	readyCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := client.Ping(readyCtx).Err(); err == nil {
			if err := seedRedis(readyCtx, client); err != nil {
				p.stop()
				return nil, fmt.Errorf("seed Redis: %w", err)
			}

			return p, nil
		}

		select {
		case <-p.done:
			cancel()
			return nil, p.err
		case <-readyCtx.Done():
			p.stop()
			return nil, fmt.Errorf("wait for Redis: %w", readyCtx.Err())
		case <-ticker.C:
		}
	}
}

func (p *redisProcess) stop() {
	p.cancel()
	<-p.done
}

func (p *redisProcess) failure() error {
	select {
	case <-p.done:
		return p.err
	default:
		return nil
	}
}

func (p *redisProcess) connect(ctx context.Context) (net.Conn, error) {
	return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", p.socket)
}
