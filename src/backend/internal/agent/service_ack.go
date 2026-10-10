package agent

import (
	"fmt"
	"honey-forge/modules/commands"
)

func (s *Service) PendingResults() []commands.AgentResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.journal.LoadState()
	results := []commands.AgentResult{}
	for id := range state.Awaiting {
		if result, ok := state.Results[id]; ok {
			results = append(results, result)
		}
	}
	return results
}
func (s *Service) AckResult(id string, status commands.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.journal.LoadState()
	result, ok := state.Results[id]
	if !ok || result.Status != status {
		return fmt.Errorf("unexpected command acknowledgement")
	}
	if !state.Awaiting[id] {
		return nil
	}
	delete(state.Awaiting, id)
	return s.journal.SaveState(state)
}
