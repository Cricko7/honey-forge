package agent

import "honey-forge/modules/commands"

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
		if copy.Code == "buffer_full" {
			runtime.BufferState = "full"
		}
	}
	return runtime
}

func (s *Service) Runtime() commands.AgentRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtime()
}
func (s *Service) PollRunner(p *Process) error { s.mu.Lock(); defer s.mu.Unlock(); return p.Poll() }

func (s *Service) Failed(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = runtimeError(code)
	if s.running {
		if err := s.runner.Stop(); err != nil && code != "buffer_unavailable" && code != "buffer_full" {
			s.failure = runtimeError("runtime_stop_failed")
		}
		s.running = false
	}
	if err := s.journal.RecoverSessions(); err != nil {
		s.failure = runtimeError("buffer_unavailable")
	}
	state := s.journal.LoadState()
	state.Running = false
	if err := s.journal.SaveState(state); err != nil {
		s.failure = runtimeError("buffer_unavailable")
	}
}

func (s *Service) RecoverBuffer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.journal.mu.Lock()
	healthy := s.journal.broken == nil
	s.journal.mu.Unlock()
	if healthy && s.failure != nil && (s.failure.Code == "buffer_unavailable" || s.failure.Code == "buffer_full") {
		if err := s.journal.RecoverSessions(); err != nil {
			s.failure = runtimeError("buffer_unavailable")
			return
		}
		state := s.journal.LoadState()
		if state.Running {
			state.Running = false
			if err := s.journal.SaveState(state); err != nil {
				s.failure = runtimeError("buffer_unavailable")
				return
			}
		}
	}
	count, _ := s.journal.Usage()
	healthy = healthy && count == 0
	if healthy && s.failure != nil && (s.failure.Code == "buffer_unavailable" || s.failure.Code == "buffer_full") {
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
