package redistrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"honey-forge/modules/profiles"
)

func clip(value string, limit int) (string, bool) {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= limit {
		return value, false
	}

	cut := value[:limit]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}

	return cut, true
}

func serve(ctx context.Context, conn net.Conn, service string, port int, snapshot profiles.Snapshot, sink Sink, dial func(context.Context) (net.Conn, error)) (result error) {
	e, err := newEmitter(ctx, conn, port, snapshot, sink)
	if err != nil {
		return err
	}

	if err := e.emit("service.connection_opened", map[string]any{"service": service}); err != nil {
		return err
	}

	started, reason := time.Now(), "peer_closed"
	var received int64
	defer func() {
		result = errors.Join(result, e.emit("service.connection_closed", map[string]any{
			"service": service, "duration_ms": time.Since(started).Milliseconds(), "bytes_received": received, "reason": reason,
		}))
	}()

	backend, err := dial(ctx)
	if err != nil {
		reason = "network_error"
		return fmt.Errorf("connect to Redis: %w", err)
	}

	defer backend.Close()
	observer := &sessionObserver{emitter: e, service: service}
	replies := make(chan struct{})
	var replyErr error
	go func() {
		defer close(replies)
		defer conn.Close()
		reader := bufio.NewReaderSize(io.TeeReader(backend, conn), 8192)
		for {
			response, err := readResponse(reader)
			if response.count > 0 {
				if err != nil {
					response.kind = 0
				}

				if emitErr := observer.reply(response); emitErr != nil {
					replyErr = emitErr
					return
				}
			}

			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					// Unsupported framing must not interrupt the genuine Redis stream.
					observer.mu.Lock()
					replyErr = observer.flush()
					observer.mu.Unlock()
					if replyErr == nil {
						replyErr = captureResponseChunks(reader, observer)
					}
				}

				return
			}
		}
	}()
	defer func() {
		_ = backend.Close()
		_ = conn.Close()
		<-replies
		result = errors.Join(result, replyErr, observer.flush())
	}()

	// Forward original bytes, including binary values and malformed frames.
	// Commands and replies are implemented exclusively by redis-server.
	reader := bufio.NewReaderSize(io.TeeReader(conn, backend), 8192)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			if ctx.Err() != nil {
				reason = "service_stopped"
				return nil
			}

			if errors.Is(err, net.ErrClosed) {
				return nil
			}

			reason = "network_error"
			return err
		}

		args, count, truncated, err := readCommand(reader)
		received += count
		if count > 0 {
			unsafeFIFO := len(args) > 0 && strings.EqualFold(args[0], "AUTH") && reader.Buffered() > 0
			if err := observer.request(observedCommand{args: args, count: count, truncated: truncated, err: err, unsafeFIFO: unsafeFIFO}); err != nil {
				reason = "service_stopped"
				return err
			}
		}

		if len(args) == 1 && strings.EqualFold(args[0], "QUIT") {
			reason = "quit"
		}

		if err != nil {
			if errors.Is(err, errFrame) {
				// Allow Redis to send its own protocol error before teardown.
				select {
				case <-replies:
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
			}

			var netErr net.Error
			switch {
			case ctx.Err() != nil:
				reason = "service_stopped"
			case errors.As(err, &netErr) && netErr.Timeout():
				reason = "idle_timeout"
			case !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed):
				reason = "network_error"
			}

			return nil
		}
	}
}

func emitCommand(e *emitter, service string, args []string, count int64, truncated bool, parseErr error, outcome string) error {
	kind, input := "command", strings.Join(args, " ")
	if parseErr != nil {
		kind, input, truncated = "request", "<malformed or incomplete frame>", true
	}

	count = min(count, 8192)
	if parseErr == nil && len(args) > 0 && strings.EqualFold(args[0], "AUTH") {
		username, password := "default", ""
		if len(args) == 2 {
			password = args[1]
		} else if len(args) >= 3 {
			username, password = args[1], args[2]
		}

		return emitAuth(e, service, username, password, count, truncated, outcome)
	}

	if parseErr == nil && len(args) > 0 && strings.EqualFold(args[0], "HELLO") {
		for i := 2; i+2 < len(args); i++ {
			if strings.EqualFold(args[i], "AUTH") {
				if err := emitAuth(e, service, args[i+1], args[i+2], count, truncated, outcome); err != nil {
					return err
				}

				break
			}
		}
	}

	input, inputCut := clip(input, 4096)
	return e.emit("service.action", map[string]any{
		"service": service, "action_kind": kind, "input": input, "outcome": outcome,
		"truncated": truncated || inputCut, "received_bytes": count,
	})
}

func emitAuth(e *emitter, service, username, password string, count int64, truncated bool, outcome string) error {
	user, userCut := clip(username, 1024)
	pass, passCut := clip(password, 1024)

	return e.emit("service.auth_attempt", map[string]any{
		"service": service, "username": user, "password": pass, "outcome": outcome,
		"truncated": truncated || userCut || passCut, "received_bytes": count,
	})
}
