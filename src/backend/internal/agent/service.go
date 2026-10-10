package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"honey-forge/internal/tcptrap"
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

func (s *Service) runtime() commands.AgentRuntime {
	count, size := s.journal.Usage()
	state := s.journal.LoadState()
	runtime := commands.AgentRuntime{RuntimeState: "stopped", BufferedEvents: count, BufferBytes: size, BufferCapacityBytes: s.journal.capacity, BufferState: "ok"}
	if state.Configuration != nil {
		revision := state.Configuration.ProfileRevision
		runtime.AppliedProfileRevision = &revision
	}
	if s.running {
		runtime.RuntimeState = "running"
	}
	if s.failure != nil {
		copy := *s.failure
		runtime.RuntimeState = "error"
		runtime.LastError = &copy
		if copy.Code == "buffer_unavailable" {
			runtime.BufferState = "unavailable"
		}
	}
	return runtime
}

func (s *Service) Runtime() commands.AgentRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtime()
}

func (s *Service) FlushInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	configuration := s.journal.LoadState().Configuration
	if configuration == nil {
		return 100 * time.Millisecond
	}
	config, err := tcptrap.ParseConfig(context.Background(), *configuration)
	if err != nil {
		return 100 * time.Millisecond
	}
	return time.Duration(config.Management.Flush) * time.Millisecond
}

func runtimeError(code string) *commands.RuntimeError {
	e := &commands.RuntimeError{Code: code}
	if err := commands.NormalizeRuntimeError(e); err != nil {
		return &commands.RuntimeError{Code: "runtime_start_failed", Message: "Runtime could not start"}
	}
	return e
}

// Execute writes the command intent before any listener side effect. Terminal
// outcomes are durable and replayed with the latest lease and observed runtime.
func (s *Service) Execute(ctx context.Context, dispatch commands.Dispatch) (commands.AgentResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.journal.LoadState()
	if stored, ok := state.Results[dispatch.CommandID]; ok {
		stored.LeaseID = dispatch.LeaseID
		stored.Runtime = s.runtime()
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
		if _, err := tcptrap.ParseConfig(ctx, *dispatch.Configuration); err != nil {
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
		if err := s.journal.SaveState(state); err != nil {
			s.failure = runtimeError("buffer_unavailable")
			return result, err
		}
		if wasRunning {
			if err := s.runner.Start(ctx, *state.Configuration); err != nil {
				state.Configuration = previous
				fail("config_apply_failed")
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
		if s.failure != nil && s.failure.Code == "buffer_unavailable" {
			fail("buffer_unavailable")
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

func (s *Service) Failed(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = runtimeError(code)
	if s.running {
		if err := s.runner.Stop(); err != nil && code != "buffer_unavailable" {
			s.failure = runtimeError("runtime_stop_failed")
		}
		s.running = false
	}
	if err := s.journal.RecoverSessions(); err != nil {
		s.failure = runtimeError("buffer_unavailable")
	}
}

func (s *Service) RecoverBuffer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.journal.mu.Lock()
	healthy := s.journal.broken == nil && len(s.journal.pending) == 0
	s.journal.mu.Unlock()
	if healthy && s.failure != nil && s.failure.Code == "buffer_unavailable" {
		s.failure = nil
	}
}

func (s *Service) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	err := s.runner.Stop()
	s.running = false
	return err
}
