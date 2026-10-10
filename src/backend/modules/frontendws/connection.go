package frontendws

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/internal/stream"
)

func (h *Handler) connected(ctx context.Context, socket *stream.Socket, cookie string, principal contract.Principal, request contract.Envelope, after, boundary int64, replayed bool, closeConnection func(int, string)) {
	verify := func() bool {
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		current, err := h.authenticate(work, cookie)
		cancel()
		if err != nil {
			var api *contract.Error
			if errors.As(err, &api) && (api.Code == "unauthenticated" || api.Code == "forbidden") {
				closeConnection(4401, "unauthenticated")
			} else {
				h.streamFailure(err, closeConnection)
			}
			return false
		}
		if current.OrganizationID != principal.OrganizationID || current.UserID != principal.UserID {
			closeConnection(4401, "unauthenticated")
			return false
		}
		return true
	}
	// Only the reader touches read deadlines and pong handlers. An idle connection
	// can wait until the next ping; heartbeat enforces the ten-second pong window.
	if err := socket.SetReadDeadline(time.Time{}); err != nil {
		h.log(err)
		return
	}
	var pong atomic.Int64
	socket.SetPongHandler(func() { pong.Store(time.Now().UnixNano()) })
	q := newQueue()
	var workers sync.WaitGroup
	fail := func(err error) { h.streamFailure(err, closeConnection) }
	workers.Go(func() { h.produce(ctx, request, after, boundary, replayed, q, fail) })
	workers.Go(func() {
		e, err := socket.Read(ctx)
		if err == nil {
			h.protocolFailure(ctx, socket, e, "invalid_message", closeConnection)
		} else {
			var api *contract.Error
			if errors.As(err, &api) {
				h.protocolFailure(ctx, socket, e, "invalid_message", closeConnection)
			} else {
				closeConnection(1000, "normal")
			}
		}
	})
	workers.Go(func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !verify() {
					return
				}
				sent := time.Now().UnixNano()
				if err := socket.Ping(time.Now().Add(time.Second)); err != nil {
					fail(err)
					return
				}
				timer := time.NewTimer(10 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
					if pong.Load() < sent {
						closeConnection(4408, "timeout")
						return
					}
				}
			}
		}
	})
	defer func() { closeConnection(1000, "normal"); workers.Wait() }()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-q.messages:
			if !verify() {
				return
			}
			err := socket.Write(ctx, item.message)
			q.release(item)
			if err != nil {
				fail(err)
				return
			}
		}
	}
}
