package frontendws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/stream"
)

func TestQueueLimits(t *testing.T) {
	for _, tt := range []struct {
		name        string
		size, limit int
	}{{"count", 1, 1000}, {"bytes", 1024 * 1024, 4}} {
		t.Run(tt.name, func(t *testing.T) {
			q := newQueue()
			for i := 0; i < tt.limit; i++ {
				if !q.push(contract.Envelope{}, tt.size) {
					t.Fatalf("early limit %d", i)
				}
			}
			if q.push(contract.Envelope{}, tt.size) {
				t.Fatal("overflow accepted")
			}
			p := <-q.messages
			q.release(p)
			if !q.push(contract.Envelope{}, tt.size) {
				t.Fatal("capacity not released")
			}
		})
	}
}

func TestReadyDoesNotConsumeNotificationCapacity(t *testing.T) {
	q := newQueue()
	for range maxPending {
		if !q.push(contract.Envelope{Type: "trap.changed"}, 1) {
			t.Fatal("notification limit reached early")
		}
	}
	if !q.push(contract.Envelope{Type: "stream.ready"}, 1) {
		t.Fatal("ready counted as a notification")
	}
	if q.push(contract.Envelope{Type: "trap.changed"}, 1) {
		t.Fatal("notification overflow accepted")
	}
}

type fakeJournal struct {
	changes []Change
	live    chan struct{}
	err     error
}

func (f *fakeJournal) Boundary(ctx context.Context) (int64, int64, error) {
	select {
	case <-f.live:
		return 2, 0, f.err
	default:
		return 1, 0, f.err
	}
}
func (f *fakeJournal) Changes(ctx context.Context, after, end int64) ([]Change, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []Change
	for _, c := range f.changes {
		if c.Sequence > after && c.Sequence <= end {
			out = append(out, c)
		}
	}
	return out, nil
}

func TestReplayReadyLive(t *testing.T) {
	codec, err := contract.NewCursorCodec([]byte("integration-cursor-key-32-bytes!"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeJournal{live: make(chan struct{}), changes: []Change{{1, "profile.changed", time.Now(), json.RawMessage(`{"revision":1}`)}, {2, "profile.deleted", time.Now(), json.RawMessage(`{}`)}}}
	h := &Handler{journal: f, cursors: codec}
	org := contract.NewID()
	ctx, cancel := context.WithCancel(contract.WithPrincipal(t.Context(), contract.Principal{OrganizationID: org, Role: contract.Admin}))
	defer cancel()
	q := newQueue()
	done := make(chan struct{})
	failure := make(chan error, 1)
	request := contract.Envelope{MessageID: contract.NewID()}
	go func() { defer close(done); h.produce(ctx, request, 0, 1, true, q, func(e error) { failure <- e }) }()
	defer func() { cancel(); <-done }()
	close(f.live) // A commit during replay must follow ready's fixed boundary.
	for i, kind := range []string{"profile.changed", "stream.ready", "profile.deleted"} {
		select {
		case item := <-q.messages:
			q.release(item)
			if item.message.Type != kind {
				t.Fatalf("type %s want %s", item.message.Type, kind)
			}
			var token string
			if err := json.Unmarshal(item.message.Payload["cursor"], &token); err != nil {
				t.Fatal(err)
			}
			seq, err := stream.DecodeCursor(codec, org, token, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if i == 2 {
				want = 2
			}
			if seq != want {
				t.Fatalf("sequence %d want %d", seq, want)
			}
			if i == 1 && (item.message.ReplyTo == nil || *item.message.ReplyTo != request.MessageID) {
				t.Fatal("ready correlation")
			}
		case e := <-failure:
			t.Fatal(e)
		case <-time.After(2 * time.Second):
			t.Fatal("delivery timeout")
		}
	}
}
