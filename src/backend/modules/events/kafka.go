package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"honey-forge/internal/contract"
	"honey-forge/modules/agentws"
	"honey-forge/modules/catalog"
)

// BatchPublisher journals validated envelopes. Broker acknowledgement alone is
// never sufficient to acknowledge telemetry to an agent.
type BatchPublisher interface {
	Publish(context.Context, agentws.Identity, string, []AgentEvent) error
}

type journalEventStore interface {
	eventStore
	Prepare(context.Context, agentws.Identity, string, string, []AgentEvent) error
}

type journalStore struct {
	store     journalEventStore
	publisher BatchPublisher
}

func (s journalStore) Ingest(ctx context.Context, identity agentws.Identity, connection, id string, events []AgentEvent) (agentws.TelemetryAck, error) {
	if err := s.store.Prepare(ctx, identity, connection, id, events); err != nil {
		return agentws.TelemetryAck{}, err
	}
	if err := s.publisher.Publish(ctx, identity, id, events); err != nil {
		if ctx.Err() != nil {
			return agentws.TelemetryAck{}, ctx.Err()
		}
		return agentws.TelemetryAck{}, fmt.Errorf("publish telemetry: %w", errors.Join(contract.NewError("telemetry_unavailable"), err))
	}
	return s.store.Ingest(ctx, identity, connection, id, events)
}

// NewJournalService uses synchronous materialization after the Kafka journal.
// A failed/lost response is recovered by the agent retrying the same immutable
// batch; PostgreSQL fingerprints enforce a single effect even across restarts.
func NewJournalService(repository *Repository, cat *catalog.Service, publisher BatchPublisher) *Service {
	return NewService(journalStore{repository, publisher}, cat)
}

type KafkaPublisher struct {
	client *kgo.Client
	topic  string
}

func NewKafkaPublisher(brokers []string, topic string) (*KafkaPublisher, error) {
	if len(brokers) == 0 || topic == "" {
		return nil, fmt.Errorf("Kafka brokers and topic are required")
	}
	for _, broker := range brokers {
		if strings.TrimSpace(broker) == "" {
			return nil, fmt.Errorf("Kafka broker must not be empty")
		}
	}
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.AllowAutoTopicCreation(), kgo.RequiredAcks(kgo.AllISRAcks()), kgo.RecordDeliveryTimeout(8*time.Second), kgo.ProducerBatchMaxBytes(512<<10))
	if err != nil {
		return nil, fmt.Errorf("configure Kafka: %w", err)
	}
	return &KafkaPublisher{client, topic}, nil
}

func (p *KafkaPublisher) Close() { p.client.Close() }

func (p *KafkaPublisher) Publish(ctx context.Context, identity agentws.Identity, batchID string, events []AgentEvent) error {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	// JSON HTML escaping can multiply a captured text's size past Kafka's batch
	// limit even when the incoming WSS frame meets the 256 KiB contract.
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(struct {
		OrganizationID string       `json:"organization_id"`
		TrapID         string       `json:"trap_id"`
		BatchID        string       `json:"batch_id"`
		Events         []AgentEvent `json:"events"`
	}{identity.OrganizationID, identity.TrapID, batchID, events})
	if err != nil {
		return fmt.Errorf("encode Kafka batch: %w", err)
	}
	result := make(chan error, 1)
	p.client.TryProduce(ctx, &kgo.Record{Topic: p.topic, Key: []byte(identity.TrapID), Value: encoded.Bytes()}, func(_ *kgo.Record, err error) { result <- err })
	// Idempotent Kafka delivery may continue after cancellation when the broker
	// outcome is ambiguous. The caller still gets its deadline, and no DB ACK.
	select {
	case <-ctx.Done():
		return fmt.Errorf("await Kafka batch: %w", ctx.Err())
	case err := <-result:
		if err != nil {
			return fmt.Errorf("journal Kafka batch: %w", err)
		}
	}
	return nil
}
