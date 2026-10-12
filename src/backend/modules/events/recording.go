package events

import (
	"context"

	"honey-forge/internal/contract"
)

// Recording is ordered by session_sequence, not receipt time. Gaps and an
// unfinished connection are explicit, so exports never claim a complete replay.
type Recording struct {
	TrapID    string  `json:"trap_id"`
	SessionID string  `json:"session_id"`
	Complete  bool    `json:"complete"`
	Events    []Event `json:"events"`
}

type recordingStore interface {
	Recording(context.Context, string, string) ([]Event, error)
}

type RecordingService struct{ repository recordingStore }

func NewRecordingService(repository recordingStore) *RecordingService {
	return &RecordingService{repository}
}

func (s *RecordingService) Read(ctx context.Context, trapID, sessionID string) (Recording, error) {
	if _, err := readAccess(ctx); err != nil {
		return Recording{}, err
	}

	if !contract.ValidID(trapID) || !contract.ValidID(sessionID) {
		return Recording{}, contract.NewError("invalid_query")
	}

	events, err := s.repository.Recording(ctx, trapID, sessionID)
	if err != nil {
		return Recording{}, err
	}

	if len(events) == 0 {
		return Recording{}, contract.NewError("resource_not_found")
	}

	complete := events[0].EventType == "service.connection_opened" && events[len(events)-1].EventType == "service.connection_closed"
	for i, e := range events {
		if e.SessionSequence != int64(i+1) {
			complete = false
		}
	}

	return Recording{TrapID: trapID, SessionID: sessionID, Complete: complete, Events: events}, nil
}
