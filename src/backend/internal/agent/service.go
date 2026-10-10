package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
)

type Runner interface {
	Start(context.Context, profiles.Snapshot) error
	Stop() error
}

type Service struct {
	mu      sync.Mutex
	journal *Journal
	runner  Runner
	running bool
	failure *commands.RuntimeError
}

func NewService(j *Journal, r Runner) *Service { return &Service{journal: j, runner: r} }

// Execute writes the command intent before any listener side effect. Terminal
// outcomes are durable and replayed with the latest lease and observed runtime.
func (s *Service) Execute(ctx context.Context, dispatch commands.Dispatch) (commands.AgentResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.journal.LoadState()
	if stored, ok := state.Results[dispatch.CommandID]; ok {
		stored.LeaseID = dispatch.LeaseID
		stored.Runtime = s.runtime()
		state.Results[dispatch.CommandID] = stored
		if state.Awaiting == nil {
			state.Awaiting = map[string]bool{}
		}
		state.Awaiting[dispatch.CommandID] = true
		if err := s.journal.SaveState(state); err != nil {
			return commands.AgentResult{}, err
		}
		return stored, nil
	}
	if !time.Now().Before(dispatch.ExpiresAt) {
		return commands.AgentResult{}, fmt.Errorf("command expired")
	}
	state.Intent = &dispatch
	if err := s.journal.SaveState(state); err != nil {
		return commands.AgentResult{}, err
	}
	result := commands.AgentResult{CommandID: dispatch.CommandID, LeaseID: dispatch.LeaseID, Status: commands.Succeeded}
	fail := func(code string) { result.Status = commands.Failed; result.Error = runtimeError(code) }
	switch dispatch.Action {
	case "stop":
		if s.running {
			if err := s.runner.Stop(); err != nil {
				fail("runtime_stop_failed")
			} else {
				s.running = false
			}
		}
	case "apply_config":
		if dispatch.Configuration == nil {
			fail("config_apply_failed")
			break
		}
		if _, err := validateSnapshot(ctx, *dispatch.Configuration); err != nil {
			fail("config_apply_failed")
			break
		}
		wasRunning := s.running
		if wasRunning {
			if err := s.runner.Stop(); err != nil {
				fail("runtime_stop_failed")
				break
			}
			s.running = false
		}
		previous := state.Configuration
		state.Configuration = dispatch.Configuration
		if wasRunning {
			if err := s.runner.Start(ctx, *state.Configuration); err != nil {
				state.Configuration = previous
				fail("port_unavailable")
				if previous != nil {
					if rollbackErr := s.runner.Start(ctx, *previous); rollbackErr != nil {
						s.failure = runtimeError("config_rollback_failed")
						fail("config_rollback_failed")
					} else {
						s.running = true
					}
				}
			} else {
				s.running = true
			}
		}
	case "start":
		if state.Configuration == nil {
			fail("runtime_start_failed")
			break
		}
		if s.failure != nil && (s.failure.Code == "buffer_unavailable" || s.failure.Code == "buffer_full") {
			fail(s.failure.Code)
			break
		}
		if !s.running {
			if err := s.runner.Start(ctx, *state.Configuration); err != nil {
				fail("port_unavailable")
			} else {
				s.running = true
			}
		}
	default:
		fail("unsupported_action")
	}
	if result.Status == commands.Succeeded {
		s.failure = nil
	}
	// Save the installed snapshot before describing the applied revision.
	state.Running = s.running
	if err := s.journal.SaveState(state); err != nil {
		s.failure = runtimeError("buffer_unavailable")
		if s.running {
			s.runner.Stop()
			s.running = false
		}
		return result, err
	}
	result.Runtime = s.runtime()
	if result.Status == commands.Succeeded {
		raw, err := json.Marshal(map[string]any{"runtime_state": result.Runtime.RuntimeState, "applied_profile_revision": result.Runtime.AppliedProfileRevision})
		if err != nil {
			return result, fmt.Errorf("encode command result: %w", err)
		}
		result.Result = raw
	}
	state.Results[dispatch.CommandID] = result
	if state.Awaiting == nil {
		state.Awaiting = map[string]bool{}
	}
	state.Awaiting[dispatch.CommandID] = true
	state.Intent = nil
	if err := s.journal.SaveState(state); err != nil {
		s.failure = runtimeError("buffer_unavailable")
		if s.running {
			s.runner.Stop()
			s.running = false
		}
		return result, err
	}
	return result, nil
}
