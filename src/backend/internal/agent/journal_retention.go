package agent

import (
	"maps"
	"time"
)

func retainedState(state State) (State, bool) {
	now := time.Now()
	for id := range state.Results {
		expires, known := state.ResultExpiresAt[id]
		if !known || !state.Awaiting[id] && !now.Before(expires) {
			state.Results = maps.Clone(state.Results)
			state.ResultExpiresAt = maps.Clone(state.ResultExpiresAt)
			return state, pruneResults(&state, now)
		}
	}

	return state, false
}

// Keep replay outcomes through the command's entire dispatch lifetime. Never
// discard an outcome still awaiting the center's durable acknowledgement.
func pruneResults(state *State, now time.Time) bool {
	changed := false
	if state.ResultExpiresAt == nil {
		state.ResultExpiresAt = make(map[string]time.Time)
	}

	for id := range state.Results {
		expires, ok := state.ResultExpiresAt[id]
		if !ok {
			state.ResultExpiresAt[id] = now.Add(24 * time.Hour)
			changed = true
			continue
		}

		if !state.Awaiting[id] && !now.Before(expires) {
			delete(state.Results, id)
			delete(state.ResultExpiresAt, id)
			changed = true
		}
	}

	return changed
}
