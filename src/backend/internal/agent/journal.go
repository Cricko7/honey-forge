// Package agent runs one registered trap and relays its durable telemetry.
package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/commands"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

const sessionReserve = 2048

type Batch struct {
	BatchID string              `json:"batch_id"`
	Events  []events.AgentEvent `json:"events"`
}

type State struct {
	Configuration *profiles.Snapshot              `json:"configuration"`
	Results       map[string]commands.AgentResult `json:"results"`
	Intent        *commands.Dispatch              `json:"intent"`
}

type record struct {
	Identity string `json:"identity,omitempty"`
	Batch    *Batch `json:"batch,omitempty"`
	Ack      string `json:"ack,omitempty"`
	State    *State `json:"state,omitempty"`
}

type activeSession struct {
	Last    events.AgentEvent
	Started time.Time
	Bytes   int64
}

type Journal struct {
	mu       sync.Mutex
	file     *os.File
	capacity int64
	pending  []Batch
	bytes    int64
	sessions map[string]bool
	active   map[string]activeSession
	state    State
	broken   error
}

func OpenJournal(path, identity string, capacity int64) (*Journal, error) {
	if identity == "" || capacity < 32*1024 {
		return nil, fmt.Errorf("journal identity and capacity are required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create journal directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	j := &Journal{file: f, capacity: capacity, sessions: map[string]bool{}, active: map[string]activeSession{}, state: State{Results: map[string]commands.AgentResult{}}}
	failed := true
	defer func() {
		if failed {
			f.Close()
		}
	}()
	if err := lockJournal(f); err != nil {
		return nil, fmt.Errorf("lock journal: %w", err)
	}
	reader := bufio.NewReader(f)
	var offset int64
	foundIdentity := ""
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			// An interrupted final append was never locally acknowledged.
			if len(line) > 0 {
				if err := f.Truncate(offset); err != nil {
					return nil, fmt.Errorf("repair journal tail: %w", err)
				}
			}
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read journal: %w", err)
		}
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("corrupt journal at byte %d: %w", offset, err)
		}
		if rec.Identity != "" {
			foundIdentity = rec.Identity
		}
		j.apply(rec)
		offset += int64(len(line))
	}
	if foundIdentity != "" && foundIdentity != identity {
		return nil, fmt.Errorf("journal belongs to another trap")
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return nil, fmt.Errorf("seek journal: %w", err)
	}
	if foundIdentity == "" {
		if err := j.append(record{Identity: identity}); err != nil {
			return nil, err
		}
	}
	failed = false
	return j, nil
}

func (j *Journal) append(rec record) error {
	if j.broken != nil {
		return j.broken
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode journal: %w", err)
	}
	raw = append(raw, '\n')
	// Detach the stored state from maps/slices owned by the caller.
	var committed record
	if err := json.Unmarshal(raw, &committed); err != nil {
		return fmt.Errorf("decode journal record: %w", err)
	}
	if _, err := j.file.Write(raw); err != nil {
		j.broken = fmt.Errorf("write journal: %w", err)
		return j.broken
	}
	if err := j.file.Sync(); err != nil {
		j.broken = fmt.Errorf("sync journal: %w", err)
		return j.broken
	}
	j.apply(committed)
	return nil
}

func batchBytes(batch Batch) int64 { raw, _ := json.Marshal(batch); return int64(len(raw)) }

func (j *Journal) apply(rec record) {
	if rec.State != nil {
		j.state = *rec.State
		if j.state.Results == nil {
			j.state.Results = map[string]commands.AgentResult{}
		}
	}
	if rec.Batch != nil {
		j.pending = append(j.pending, *rec.Batch)
		j.bytes += batchBytes(*rec.Batch)
		for _, e := range rec.Batch.Events {
			if e.EventType == "tcp.connection_opened" || e.EventType == "service.connection_opened" {
				j.sessions[e.SessionID] = true
				started, _ := time.Parse(time.RFC3339Nano, e.OccurredAt)
				j.active[e.SessionID] = activeSession{Last: e, Started: started}
			}
			if e.EventType == "tcp.payload_received" {
				a := j.active[e.SessionID]
				a.Last = e
				var d struct {
					Original int64 `json:"original_bytes"`
				}
				json.Unmarshal(e.Data, &d)
				a.Bytes += d.Original
				j.active[e.SessionID] = a
			}
			if e.EventType == "service.auth_attempt" || e.EventType == "service.action" {
				a := j.active[e.SessionID]
				a.Last = e
				var d struct {
					Received int64 `json:"received_bytes"`
				}
				json.Unmarshal(e.Data, &d)
				a.Bytes += d.Received
				j.active[e.SessionID] = a
			}
			if e.EventType == "tcp.connection_closed" || e.EventType == "service.connection_closed" {
				delete(j.sessions, e.SessionID)
				delete(j.active, e.SessionID)
			}
		}
	}
	if rec.Ack != "" {
		for i, b := range j.pending {
			if b.BatchID == rec.Ack {
				j.bytes -= batchBytes(b)
				j.pending = append(j.pending[:i], j.pending[i+1:]...)
				break
			}
		}
	}
}

func (j *Journal) Enqueue(e events.AgentEvent) (Batch, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, b := range j.pending {
		for _, old := range b.Events {
			if old.EventID == e.EventID {
				a, _ := json.Marshal(old)
				v, _ := json.Marshal(e)
				if !bytes.Equal(a, v) {
					return Batch{}, fmt.Errorf("event identifier conflict")
				}
				return b, nil
			}
		}
	}
	b := Batch{BatchID: string(contract.NewID()), Events: []events.AgentEvent{e}}
	reserve := int64(len(j.sessions)) * sessionReserve
	if (e.EventType == "tcp.connection_opened" || e.EventType == "service.connection_opened") && !j.sessions[e.SessionID] {
		reserve += sessionReserve
	}
	if (e.EventType == "tcp.connection_closed" || e.EventType == "service.connection_closed") && j.sessions[e.SessionID] {
		reserve -= sessionReserve
	}
	if j.bytes+batchBytes(b)+reserve > j.capacity {
		return Batch{}, fmt.Errorf("telemetry buffer is full")
	}
	return b, j.append(record{Batch: &b})
}

func (j *Journal) Pending() []Batch {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]Batch(nil), j.pending...)
}

func (j *Journal) Ack(batchID string, ids []string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, b := range j.pending {
		if b.BatchID == batchID {
			if len(ids) != len(b.Events) {
				return fmt.Errorf("incomplete telemetry acknowledgement")
			}
			for i, e := range b.Events {
				if ids[i] != e.EventID {
					return fmt.Errorf("unexpected acknowledged event")
				}
			}
			return j.append(record{Ack: batchID})
		}
	}
	return fmt.Errorf("unknown batch acknowledgement")
}

func (j *Journal) LoadState() State {
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, _ := json.Marshal(j.state)
	var state State
	json.Unmarshal(raw, &state)
	return state
}

func (j *Journal) SaveState(state State) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.append(record{State: &state})
}

func (j *Journal) Usage() (int64, int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return int64(len(j.pending)), j.bytes
}

func (j *Journal) Close() error { j.mu.Lock(); defer j.mu.Unlock(); return j.file.Close() }
