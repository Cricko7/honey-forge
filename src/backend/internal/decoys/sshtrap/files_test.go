package sshtrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type memoryFile struct {
	writes int
	fail   bool
}

func (f *memoryFile) WriteAt(b []byte, off int64) (int, error) {
	f.writes++
	if f.fail {
		return 0, errors.New("write failed")
	}
	return len(b), nil
}
func (f *memoryFile) Close() error { return nil }

func TestUploadCapturesRandomWritesAndHashOutsideSandbox(t *testing.T) {
	var got []events.AgentEvent
	r := newRecording(t.Context(), profiles.Snapshot{TypeID: "ssh-high-interaction", TypeVersion: 1, ProfileRevision: 1}, "ssh", "127.0.0.1:12345", 2222, 8192, func(_ context.Context, e events.AgentEvent) error { got = append(got, e); return nil })
	u := &upload{file: &memoryFile{}, record: r, id: "file", limit: 32, release: func() {}, fail: func(error) {}}
	if _, err := u.WriteAt([]byte("world"), 5); err != nil {
		t.Fatal(err)
	}
	if _, err := u.WriteAt([]byte("hello"), 0); err != nil {
		t.Fatal(err)
	}
	if err := u.Close(); err != nil {
		t.Fatal(err)
	}
	var closed struct {
		Size     int    `json:"size"`
		Hash     string `json:"sha256"`
		Complete bool   `json:"complete"`
	}
	if err := json.Unmarshal(got[len(got)-1].Data, &closed); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("helloworld"))
	if closed.Size != 10 || closed.Hash != hex.EncodeToString(sum[:]) || !closed.Complete {
		t.Fatalf("artifact: %+v", closed)
	}
	if err := u.Close(); err != nil || len(got) != 3 {
		t.Fatal("duplicate close event")
	}
}

func TestUploadLimitsAndCommitFailurePreventSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name     string
		offset   int64
		size     int
		sinkFail bool
	}{{"negative offset", -1, 1, false}, {"sparse allocation", 31, 2, false}, {"oversized upload", 0, 33, false}, {"journal failure", 0, 1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			file := &memoryFile{}
			r := newRecording(t.Context(), profiles.Snapshot{}, "ssh", "127.0.0.1:12345", 2222, 8192, func(context.Context, events.AgentEvent) error {
				if tc.sinkFail {
					return errors.New("journal unavailable")
				}
				return nil
			})
			u := &upload{file: file, record: r, id: "file", limit: 32, release: func() {}, fail: func(error) {}}
			if _, err := u.WriteAt(make([]byte, tc.size), tc.offset); err == nil {
				t.Fatal("invalid upload accepted")
			}
			if file.writes != 0 {
				t.Fatal("unrecorded data reached sandbox")
			}
		})
	}
}
