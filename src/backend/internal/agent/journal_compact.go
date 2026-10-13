package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Replace only after the checkpoint is durable. The separate owner lock stays
// held across replacement, including on Windows where the data file must close.
func (j *Journal) compactFile() error {
	if j.records < 128 {
		return nil
	}
	raw, err := json.Marshal(record{Identity: j.identity, Checkpoint: &checkpoint{Pending: j.pending, Active: j.active, State: j.state}})
	if err != nil {
		return fmt.Errorf("encode file checkpoint: %w", err)
	}
	path := j.file.Name()
	temp, err := os.CreateTemp(filepath.Dir(path), ".journal-checkpoint-")
	if err != nil {
		return fmt.Errorf("create file checkpoint: %w", err)
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(append(raw, '\n')); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Sync(); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := j.file.Close(); err != nil {
		return err
	}
	renameErr := os.Rename(name, path)
	file, openErr := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0600)
	if openErr != nil {
		return fmt.Errorf("reopen checkpoint: %w", errors.Join(renameErr, openErr))
	}
	j.file = file
	if err := lockJournal(file); err != nil {
		return fmt.Errorf("lock checkpoint: %w", err)
	}
	if renameErr != nil {
		return fmt.Errorf("replace checkpoint: %w", renameErr)
	}
	j.records = 1
	return nil
}
