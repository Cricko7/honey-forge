package redistrap

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
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

func serve(ctx context.Context, conn net.Conn, service string, port int, password string, snapshot profiles.Snapshot, sink Sink) (result error) {
	emitter, err := newEmitter(ctx, conn, port, snapshot, sink)
	if err != nil {
		return err
	}
	if err := emitter.emit("service.connection_opened", map[string]any{"service": service}); err != nil {
		return err
	}
	started := time.Now()
	reason := "peer_closed"
	var received int64
	defer func() {
		closeErr := emitter.emit("service.connection_closed", map[string]any{"service": service, "duration_ms": time.Since(started).Milliseconds(), "bytes_received": received, "reason": reason})
		result = errors.Join(result, closeErr)
	}()
	reader := bufio.NewReaderSize(conn, 8192)
	db := newDatabase()
	authenticated := false
	for {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			reason = "network_error"
			return err
		}
		args, count, err := readFrame(reader)
		received += int64(count)
		if err != nil {
			switch {
			case ctx.Err() != nil:
				reason = "service_stopped"
			case errors.Is(err, io.EOF):
				reason = "peer_closed"
			case errors.Is(err, errFrame):
				if emitErr := emitter.emit("service.action", map[string]any{"service": service, "action_kind": "command", "input": "<malformed frame>", "outcome": "rejected", "truncated": true, "received_bytes": count}); emitErr != nil {
					reason = "service_stopped"
					return emitErr
				}
				if writeErr := writeReply(conn, "-ERR Protocol error: invalid multibulk length\r\n"); writeErr != nil {
					reason = "network_error"
					return nil
				}
				reason = "network_error"
			default:
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					reason = "idle_timeout"
				} else {
					reason = "network_error"
				}
			}
			return nil
		}
		command := strings.ToUpper(args[0])
		var reply string
		var quit bool
		if command == "AUTH" {
			username := "default"
			pass := ""
			if len(args) == 2 {
				pass = args[1]
			}
			if len(args) >= 3 {
				username, pass = args[1], strings.Join(args[2:], " ")
			}
			accepted := (len(args) == 2 || len(args) == 3) && username == "default" && subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
			outcome := "rejected"
			if accepted {
				outcome = "accepted"
				authenticated = true
			}
			userValue, userCut := clip(username, 1024)
			passValue, passCut := clip(pass, 1024)
			if err := emitter.emit("service.auth_attempt", map[string]any{"service": service, "username": userValue, "password": passValue, "outcome": outcome, "truncated": userCut || passCut, "received_bytes": count}); err != nil {
				reason = "service_stopped"
				return err
			}
			if accepted {
				reply = "+OK\r\n"
			} else {
				reply = "-WRONGPASS invalid username-password pair or user is disabled.\r\n"
			}
		} else {
			accepted := false
			if !authenticated {
				reply = "-NOAUTH Authentication required.\r\n"
			} else {
				reply, accepted, quit = db.execute(args)
			}
			outcome := "rejected"
			if accepted {
				outcome = "accepted"
			}
			input, truncated := clip(strings.Join(args, " "), 4096)
			if err := emitter.emit("service.action", map[string]any{"service": service, "action_kind": "command", "input": input, "outcome": outcome, "truncated": truncated, "received_bytes": count}); err != nil {
				reason = "service_stopped"
				return err
			}
		}
		if err := writeReply(conn, reply); err != nil {
			reason = "network_error"
			return nil
		}
		if quit {
			reason = "quit"
			return nil
		}
	}
}

func writeReply(conn net.Conn, reply string) error {
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	_, err := io.WriteString(conn, reply)
	return err
}
