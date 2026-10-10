package agent

import (
	"context"
	"fmt"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
	"time"
)

// Restore runs before hello. Welcome never overrides local state or an intent.
func (s *Service) Restore(ctx context.Context) error {
	state := s.journal.LoadState()
	if state.Intent != nil && state.Intent.Action == "stop" {
		state.Running = false
	}
	if state.Running && state.Configuration != nil {
		if err := s.runner.Start(ctx, *state.Configuration); err != nil {
			s.failure = runtimeError("runtime_start_failed")
			return nil
		}
		s.running = true
	}
	if state.Intent != nil {
		intent := *state.Intent
		if !time.Now().Before(intent.ExpiresAt) {
			state.Results[intent.CommandID] = commands.AgentResult{CommandID: intent.CommandID, LeaseID: intent.LeaseID, Status: commands.Failed, Error: runtimeError("command_expired"), Runtime: s.Runtime()}
			state.Intent = nil
			state.Running = s.running
			return s.journal.SaveState(state)
		}
		if _, err := s.Execute(ctx, intent); err != nil {
			return fmt.Errorf("recover command intent: %w", err)
		}
	}
	return nil
}
func (s *Service) RestoreWelcome(ctx context.Context, snapshot *profiles.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.journal.LoadState()
	if snapshot == nil || state.Configuration != nil || state.Intent != nil {
		return nil
	}
	if _, err := validateSnapshot(ctx, *snapshot); err != nil {
		return err
	}
	state.Configuration = snapshot
	state.Running = false
	return s.journal.SaveState(state)
}
