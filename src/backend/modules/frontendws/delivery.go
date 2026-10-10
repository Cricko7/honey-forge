package frontendws

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/stream"
)

const maxPending = 1000
const maxPendingBytes = 4 * 1024 * 1024

type pending struct {
	message      contract.Envelope
	bytes        int
	notification bool
}
type queue struct {
	messages chan pending
	count    atomic.Int64
	bytes    atomic.Int64
}

func newQueue() *queue { return &queue{messages: make(chan pending, maxPending+1)} }
func (q *queue) push(e contract.Envelope, size int) bool {
	item := pending{message: e, bytes: size, notification: e.Type != "stream.ready"}
	count := q.count.Load()
	if item.notification {
		count = q.count.Add(1)
	}
	bytes := q.bytes.Add(int64(size))
	if count > maxPending || bytes > maxPendingBytes {
		q.release(item)
		return false
	}
	select {
	case q.messages <- item:
		return true
	default:
		q.release(item)
		return false
	}
}
func (q *queue) release(item pending) {
	if item.notification {
		q.count.Add(-1)
	}
	q.bytes.Add(-int64(item.bytes))
}

func (h *Handler) produce(ctx context.Context, request contract.Envelope, after, boundary int64, replayed bool, q *queue, fail func(error)) {
	p, _ := contract.PrincipalFrom(ctx)
	enqueue := func(kind string, reply *contract.ID, payload any) bool {
		e, size, err := envelope(reply, kind, payload)
		if err != nil {
			fail(err)
			return false
		}
		if !q.push(e, size) {
			fail(errSlowConsumer)
			return false
		}
		return true
	}
	deliver := func(end int64) bool {
		for after < end {
			work, cancel := context.WithTimeout(ctx, 5*time.Second)
			changes, err := h.journal.Changes(work, after, end)
			cancel()
			if err != nil {
				fail(err)
				return false
			}
			if len(changes) == 0 {
				fail(fmt.Errorf("stream journal contains a gap"))
				return false
			}
			for _, change := range changes {
				if change.Sequence != after+1 {
					fail(fmt.Errorf("stream journal is out of order"))
					return false
				}
				cursor, err := stream.EncodeCursor(h.cursors, p.OrganizationID, change.Sequence, time.Now())
				if err != nil {
					fail(err)
					return false
				}
				if !enqueue(change.Type, nil, notification{cursor, change.OccurredAt.UTC(), change.Data}) {
					return false
				}
				after = change.Sequence
			}
		}
		return true
	}
	if !deliver(boundary) {
		return
	}
	now := time.Now().UTC()
	cursor, err := stream.EncodeCursor(h.cursors, p.OrganizationID, boundary, now)
	if err != nil {
		fail(err)
		return
	}
	if !enqueue("stream.ready", &request.MessageID, ready{cursor, now, replayed}) {
		return
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			work, cancel := context.WithTimeout(ctx, 5*time.Second)
			end, _, err := h.journal.Boundary(work)
			cancel()
			if err != nil {
				fail(err)
				return
			}
			if !deliver(end) {
				return
			}
		}
	}
}
