//go:build integration

package events

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
)

func TestKafkaJournal(t *testing.T) {
	brokers := os.Getenv("TEST_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("TEST_KAFKA_BROKERS required")
	}
	topic := "honey-forge-test-" + string(contract.NewID())
	publisher, err := NewKafkaPublisher(strings.Split(brokers, ","), topic)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)
	reader, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(brokers, ",")...), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	identity := agentws.Identity{TrapID: string(contract.NewID()), OrganizationID: string(contract.NewID())}
	id := string(contract.NewID())
	if err := publisher.Publish(ctx, identity, id, []AgentEvent{{EventID: string(contract.NewID()), Data: json.RawMessage(`{"username":" Root ","password":"original"}`)}}); err != nil {
		t.Fatal(err)
	}
	fetches := reader.PollRecords(ctx, 1)
	if errs := fetches.Errors(); len(errs) > 0 {
		t.Fatalf("Kafka fetch failed: %v", errs)
	}
	records := fetches.Records()
	if len(records) != 1 || string(records[0].Key) != identity.TrapID {
		t.Fatal("Kafka record/key missing")
	}
	var stored struct {
		BatchID string       `json:"batch_id"`
		TrapID  string       `json:"trap_id"`
		Events  []AgentEvent `json:"events"`
	}
	if err := json.Unmarshal(records[0].Value, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.BatchID != id || stored.TrapID != identity.TrapID || len(stored.Events) != 1 {
		t.Fatal("Kafka envelope changed")
	}
}

func TestKafkaPublishCancellation(t *testing.T) {
	publisher, err := NewKafkaPublisher([]string{"127.0.0.1:1"}, "cancellation")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := publisher.Publish(ctx, agentws.Identity{}, "batch", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestKafkaPublishDeadline(t *testing.T) {
	publisher, err := NewKafkaPublisher([]string{"127.0.0.1:1"}, "deadline")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := publisher.Publish(ctx, agentws.Identity{}, "batch", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}
