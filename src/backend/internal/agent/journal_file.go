package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"honey-forge/modules/commands"
	"io"
	"os"
	"path/filepath"
)

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
