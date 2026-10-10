package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
	"strings"
	"time"
)

// Redis must use AOF/fsync-always and no eviction: a local acknowledgement is a
// durability promise. An expiring owner lease prevents two agents sharing state.
type redisJournal struct {
	client     *redis.Client
	key, owner string
	records    int
}

const redisLeaseMilliseconds = 15000

func OpenRedisJournal(ctx context.Context, address, identity string, capacity int64) (*Journal, error) {
	if !contract.ValidID(identity) || capacity < 32*1024 {
		return nil, fmt.Errorf("Redis journal identity and capacity are required")
	}
	identity = strings.ToLower(identity)
	opts, err := redis.ParseURL(address)
	if err != nil {
		return nil, fmt.Errorf("invalid AGENT_REDIS_URL")
	}
	opts.Protocol = 2
	opts.MaxRetries = -1
	opts.ContextTimeoutEnabled = true
	opts.DialTimeout = 2 * time.Second
	opts.ReadTimeout = 2 * time.Second
	opts.WriteTimeout = 2 * time.Second
	store := &redisJournal{client: redis.NewClient(opts), key: "honey-forge:{" + identity + "}:journal", owner: string(contract.NewID())}
	failed := true
	defer func() {
		if failed {
			store.close()
		}
	}()
	for name, want := range map[string]string{"appendonly": "yes", "appendfsync": "always", "maxmemory-policy": "noeviction"} {
		values, err := store.client.ConfigGet(ctx, name).Result()
		if err != nil {
			return nil, fmt.Errorf("check Redis durability: %w", err)
		}
		if values[name] != want {
			return nil, fmt.Errorf("Redis requires %s=%s", name, want)
		}
	}
	if err := store.lease(ctx); err != nil {
		return nil, err
	}
	j := &Journal{capacity: capacity, redis: store, sessions: map[string]bool{}, active: map[string]activeSession{}, state: State{Results: map[string]commands.AgentResult{}}}
	if err := j.reloadRedis(ctx); err != nil {
		return nil, err
	}
	if err := j.append(record{Identity: identity}); err != nil {
		return nil, err
	}
	failed = false
	return j, nil
}
func (r *redisJournal) lease(ctx context.Context) error {
	result, err := r.client.Eval(ctx, `local v=redis.call('GET',KEYS[1]); if v and v~=ARGV[1] then return 0 end; redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2]); return 1`, []string{r.key + ":owner"}, r.owner, redisLeaseMilliseconds).Int()
	if err != nil {
		return fmt.Errorf("Redis lease: %w", err)
	}
	if result != 1 {
		return fmt.Errorf("Redis journal is owned by another agent")
	}
	return nil
}
func (r *redisJournal) append(raw []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := r.client.Eval(ctx, `if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end; redis.call('RPUSH',KEYS[2],ARGV[2]); redis.call('PEXPIRE',KEYS[1],ARGV[3]); return 1`, []string{r.key + ":owner", r.key}, r.owner, string(raw), redisLeaseMilliseconds).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return fmt.Errorf("Redis journal lease lost")
	}
	r.records++
	return nil
}
func (r *redisJournal) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := r.client.Eval(ctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end; return 0`, []string{r.key + ":owner"}, r.owner).Result()
	return errors.Join(err, r.client.Close())
}
func (j *Journal) reloadRedis(ctx context.Context) error {
	records, err := j.redis.client.LRange(ctx, j.redis.key, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("restore Redis journal: %w", err)
	}
	if len(records) == 0 && j.redis.records > 0 {
		return fmt.Errorf("Redis journal is missing")
	}
	restored := &Journal{sessions: map[string]bool{}, active: map[string]activeSession{}, state: State{Results: map[string]commands.AgentResult{}}}
	for _, raw := range records {
		var rec record
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return fmt.Errorf("corrupt Redis journal: %w", err)
		}
		restored.apply(rec)
	}
	j.pending, j.bytes, j.sessions, j.active, j.state = restored.pending, restored.bytes, restored.sessions, restored.active, restored.state
	j.redis.records = len(records)
	return nil
}

// Compaction retains every unacknowledged event, active session, installed
// snapshot and command result. The Redis list replacement is one atomic script.
func (j *Journal) compactRedis() error {
	if j.redis.records < 128 {
		return nil
	}
	raw, err := json.Marshal(record{Checkpoint: &checkpoint{Pending: j.pending, Active: j.active, State: j.state}})
	if err != nil {
		return fmt.Errorf("encode Redis checkpoint: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := j.redis.client.Eval(ctx, `if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end; redis.call('LSET',KEYS[2],0,ARGV[2]); redis.call('LTRIM',KEYS[2],0,0); return 1`, []string{j.redis.key + ":owner", j.redis.key}, j.redis.owner, string(raw)).Int()
	if err != nil {
		return fmt.Errorf("compact Redis journal: %w", err)
	}
	if result != 1 {
		return fmt.Errorf("Redis journal lease lost")
	}
	j.redis.records = 1
	return nil
}

// CheckStorage renews the lease even while there are no events. Any loss of
// Redis stops listeners; retained records are reloaded before accepting writes.
func (j *Journal) CheckStorage(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.redis == nil {
		return j.broken
	}
	if err := j.redis.lease(ctx); err != nil {
		j.broken = err
		return err
	}
	if j.broken != nil {
		if err := j.reloadRedis(ctx); err != nil {
			return err
		}
		j.broken = nil
	}
	return nil
}
