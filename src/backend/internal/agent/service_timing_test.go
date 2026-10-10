package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHeartbeatFollowsInstalledConfiguration(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), "trap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	s := NewService(j, &fakeRunner{})
	if got := s.HeartbeatInterval(); got != 10*time.Second {
		t.Fatal(got)
	}
	snapshot := demoSnapshot()
	if _, err := s.Execute(t.Context(), configCommand("apply_config", &snapshot)); err != nil {
		t.Fatal(err)
	}
	if got := s.HeartbeatInterval(); got != 5*time.Second {
		t.Fatal(got)
	}
}
