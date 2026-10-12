package sshtrap

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

func TestInterruptedUploadDescribesOnlyCommittedBlocks(t *testing.T) {
	var closedSize int
	r := newRecording(t.Context(), profiles.Snapshot{}, "ssh", "127.0.0.1:12345", 2222, 16384, func(_ context.Context, e events.AgentEvent) error {
		var d struct {
			Offset int `json:"offset"`
			Size   int `json:"size"`
		}
		json.Unmarshal(e.Data, &d)
		if e.EventType == "service.file_chunk" && d.Offset == 4096 {
			return errors.New("journal unavailable")
		}

		if e.EventType == "service.file_closed" {
			closedSize = d.Size
		}

		return nil
	})
	u := &upload{file: &memoryFile{}, record: r, id: "file", limit: 8192, release: func() {}, fail: func(error) {}}
	if _, err := u.WriteAt(make([]byte, 8192), 0); err == nil {
		t.Fatal("failed commit was accepted")
	}

	if err := u.Close(); err != nil {
		t.Fatal(err)
	}

	if closedSize != 4096 {
		t.Fatalf("size %d, want only the committed 4096 bytes", closedSize)
	}
}
