//go:build integration

package agent

import (
	"crypto/tls"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"honey-forge/internal/contract"
	"honey-forge/internal/platform/tlsconfig"
	"honey-forge/modules/events"
	"os"
	"strings"
	"testing"
)

func TestRedisRejectsMissingAndWrongCredentials(t *testing.T) {
	address := redisTestURL(t)
	for _, tt := range []struct {
		name, password string
		authorized     bool
	}{{"valid credentials", "", true}, {"missing password", "", false}, {"wrong password", "wrong-test-password", false}} {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := redisOptions(address, redisTestTLS(t))
			if err != nil {
				t.Fatal(err)
			}
			if tt.authorized && opts.Password == "" {
				t.Fatal("TEST_REDIS_URL must include the Redis password")
			}
			if !tt.authorized {
				opts.Username = ""
				opts.Password = tt.password
			}
			client := redis.NewClient(opts)
			defer client.Close()
			err = client.Ping(t.Context()).Err()
			if (err == nil) != tt.authorized {
				t.Fatalf("authorized=%v error=%v", tt.authorized, err)
			}
			if err != nil && !strings.Contains(err.Error(), "NOAUTH") && !strings.Contains(err.Error(), "WRONGPASS") {
				t.Fatalf("failure did not enforce authentication: %v", err)
			}
		})
	}
}

func redisTestURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_REDIS_URL")
	if u == "" {
		t.Skip("TEST_REDIS_URL required")
	}
	return u
}
func redisTestTLS(t *testing.T) *tls.Config {
	t.Helper()
	config, err := tlsconfig.Client(os.Getenv("TEST_REDIS_CA_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	return config
}
func TestRedisJournalDurabilityIdentityAndLease(t *testing.T) {
	url := redisTestURL(t)
	id := string(contract.NewID())
	j, err := OpenRedisJournal(t.Context(), url, id, 1<<20, redisTestTLS(t))
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenRedisJournal(t.Context(), url, id, 1<<20, redisTestTLS(t)); err == nil {
		other.Close()
		t.Fatal("two journal owners")
	}
	e := events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), SessionSequence: 1, OccurredAt: "2026-10-10T00:00:00Z", EventType: "tcp.connection_opened", Data: json.RawMessage(`{"listener_name":"demo"}`)}
	batch, err := j.Enqueue(e)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(j, &fakeRunner{})
	snapshot := demoSnapshot()
	snapshot.ProfileRevision = 2
	apply := configCommand("apply_config", &snapshot)
	if _, err := s.Execute(t.Context(), apply); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenRedisJournal(t.Context(), url, id, 1<<20, redisTestTLS(t))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if len(j.Pending()) != 1 || j.Pending()[0].BatchID != batch.BatchID || j.Pending()[0].Events[0].EventID != e.EventID {
		t.Fatal("lost event/batch identity")
	}
	runner := &fakeRunner{}
	s = NewService(j, runner)
	if err := s.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	apply.LeaseID = string(contract.NewID())
	if result, err := s.Execute(t.Context(), apply); err != nil || *result.Runtime.AppliedProfileRevision != 2 || runner.starts != 0 {
		t.Fatalf("replay %+v %v", result, err)
	}
	if err := j.Ack(batch.BatchID, []string{string(contract.NewID())}); err == nil || len(j.Pending()) != 1 {
		t.Fatal("wrong ack removed event")
	}
	if err := j.Ack(batch.BatchID, []string{e.EventID}); err != nil {
		t.Fatal(err)
	}
}

func TestRedisJournalStopsWritesAfterLeaseLoss(t *testing.T) {
	url := redisTestURL(t)
	id := string(contract.NewID())
	j, err := OpenRedisJournal(t.Context(), url, id, 1<<20, redisTestTLS(t))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	opts, err := redisOptions(url, redisTestTLS(t))
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()
	if err := client.Set(t.Context(), j.redis.key+":owner", "replacement", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Enqueue(events.AgentEvent{EventID: string(contract.NewID())}); err == nil {
		t.Fatal("lease loss accepted")
	}
	if len(j.Pending()) != 0 {
		t.Fatal("uncommitted event acknowledged")
	}
	if err := j.CheckStorage(t.Context()); err == nil {
		t.Fatal("stale owner recovered")
	}
}

func TestRedisCheckpointRetainsUnacknowledgedAndActiveSessions(t *testing.T) {
	url := redisTestURL(t)
	id := string(contract.NewID())
	j, err := OpenRedisJournal(t.Context(), url, id, 1<<20, redisTestTLS(t))
	if err != nil {
		t.Fatal(err)
	}
	opened := events.AgentEvent{EventID: string(contract.NewID()), SessionID: string(contract.NewID()), EventType: "tcp.connection_opened", SessionSequence: 1, OccurredAt: "2026-10-10T00:00:00Z", Data: json.RawMessage(`{"listener_name":"demo"}`)}
	b, err := j.Enqueue(opened)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Ack(b.BatchID, []string{opened.EventID}); err != nil {
		t.Fatal(err)
	}
	for range 150 {
		if err := j.SaveState(j.LoadState()); err != nil {
			t.Fatal(err)
		}
	}
	payload := opened
	payload.EventID = string(contract.NewID())
	payload.SessionSequence = 2
	payload.EventType = "tcp.payload_received"
	payload.Data = json.RawMessage(`{"listener_name":"demo","original_bytes":3}`)
	pending, err := j.Enqueue(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenRedisJournal(t.Context(), url, id, 1<<20, redisTestTLS(t))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if len(j.Pending()) != 1 || j.Pending()[0].BatchID != pending.BatchID {
		t.Fatal("checkpoint lost pending IDs")
	}
	if err := j.RecoverSessions(); err != nil {
		t.Fatal(err)
	}
	if len(j.Pending()) != 2 || j.Pending()[1].Events[0].SessionID != opened.SessionID || j.Pending()[1].Events[0].SessionSequence != 3 {
		t.Fatal("checkpoint lost active session")
	}
}
