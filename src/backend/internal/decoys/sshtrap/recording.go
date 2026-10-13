package sshtrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

var ErrRecordingLimit = errors.New("session recording limit reached")

type Sink func(context.Context, events.AgentEvent) error

type recording struct {
	ctx                                 context.Context
	snapshot                            profiles.Snapshot
	service, sessionID                  string
	source                              events.Source
	port                                int
	sink                                Sink
	mu                                  sync.Mutex // serializes event commits, sequence and recording budget
	sequence, bytes, limit              int64
	received, uploadBytes, uploadExtent int64
	failure                             func(error)
	reason                              string
}

func newRecording(ctx context.Context, snapshot profiles.Snapshot, service, peer string, port int, limit int64, sink Sink) *recording {
	ip, text, _ := net.SplitHostPort(peer)
	peerPort, _ := strconv.Atoi(text)

	return &recording{
		ctx:       ctx,
		snapshot:  snapshot,
		service:   service,
		sessionID: string(contract.NewID()),
		source:    events.Source{IP: ip, Port: peerPort},
		port:      port,
		limit:     limit,
		sink:      sink,
	}
}

func (r *recording) event(kind string, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.emit(kind, fields)
}

// emit is called with mu held; counters advance only after durable acknowledgement.
func (r *recording) emit(kind string, fields map[string]any) error {
	return r.emitAt(kind, fields, time.Now().UTC(), string(contract.NewID()))
}

func (r *recording) emitAt(kind string, fields map[string]any, at time.Time, id string) error {
	if r.sequence >= 8192 && kind != "service.connection_closed" {
		r.reason = "recording_limit"
		return ErrRecordingLimit
	}

	fields["service"] = r.service
	raw, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("encode SSH event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 5*time.Second)
	defer cancel()

	e := events.AgentEvent{
		EventID:         id,
		EventType:       kind,
		TypeID:          r.snapshot.TypeID,
		TypeVersion:     int64(r.snapshot.TypeVersion),
		ProfileRevision: int64(r.snapshot.ProfileRevision),
		OccurredAt:      at.UTC().Format(time.RFC3339Nano),
		SessionID:       r.sessionID,
		SessionSequence: r.sequence + 1,
		Source:          r.source,
		Destination:     events.Destination{Protocol: "tcp", Port: r.port},
		Data:            raw,
	}
	if err := r.sink(ctx, e); err != nil {
		return fmt.Errorf("commit SSH event: %w", err)
	}

	r.sequence++

	return nil
}

func (r *recording) io(channel, stream string, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if int64(len(data)) > r.limit-r.bytes {
		r.reason = "recording_limit"
		return ErrRecordingLimit
	}

	for len(data) > 0 {
		n := min(len(data), 4096)
		if err := r.emit("service.session_io", map[string]any{"channel_id": channel, "stream": stream, "payload_base64": base64.StdEncoding.EncodeToString(data[:n]), "bytes": n}); err != nil {
			return err
		}

		r.bytes += int64(n)
		if stream == "stdin" || stream == "exec" {
			r.received += int64(n)
		}

		data = data[n:]
	}

	return nil
}

func (r *recording) receivedBytes() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.received
}

func (r *recording) closeReason() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.reason
}

type recordingWriter struct {
	recording       *recording
	channel, stream string
	write           func([]byte) (int, error)
}

func (w recordingWriter) Write(data []byte) (int, error) {
	// Persist before forwarding so an attacker cannot erase an unrecorded action.
	if err := w.recording.io(w.channel, w.stream, data); err != nil {
		if w.recording.failure != nil {
			w.recording.failure(err)
		}

		return 0, err
	}

	return w.write(data)
}
