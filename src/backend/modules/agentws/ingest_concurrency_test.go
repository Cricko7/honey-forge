package agentws

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/stream"
)

// Stop at the acknowledgement's context check without depending on socket timing.
type blockedAckContext struct {
	context.Context
	started chan struct{}
	release chan struct{}
}

func (c *blockedAckContext) Err() error {
	close(c.started)
	<-c.release

	return context.Canceled
}

func TestIngestRetainsAdmissionSlotUntilAcknowledgementCompletes(t *testing.T) {
	ctx := &blockedAckContext{Context: context.Background(), started: make(chan struct{}), release: make(chan struct{})}
	gateway := &fakeGateway{token: "secret"}
	identity, err := gateway.Authenticate(t.Context(), "secret")
	if err != nil {
		t.Fatal(err)
	}

	session := &agentSession{identity: identity, token: "secret", ctx: ctx, socket: &stream.Socket{}}
	session.active.Store(true)
	entry := &sessionEntry{current: session}
	busy := &atomic.Bool{}
	busy.Store(true)
	done := make(chan struct{})
	go func() {
		defer close(done)

		NewHandler(gateway, &fakeCommands{}).ingest(entry, session, contract.Envelope{MessageID: contract.NewID()}, TelemetryBatch{BatchID: string(contract.NewID()), Events: []Event{{EventID: string(contract.NewID())}}}, busy)
	}()
	defer func() {
		close(ctx.release)
		<-done
	}()

	select {
	case <-ctx.started:
	case <-time.After(time.Second):
		t.Fatal("acknowledgement was not attempted")
	}

	if busy.CompareAndSwap(false, true) {
		t.Fatal("a second ingest was admitted before the first acknowledgement completed")
	}
}
