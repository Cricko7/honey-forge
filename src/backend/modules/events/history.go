package events

import "context"

type HistoryService struct{ repository *Repository }

func NewHistoryService(repository *Repository) *HistoryService { return &HistoryService{repository} }

func (s *HistoryService) Read(ctx context.Context, id string) (Event, error) {
	if _, err := readAccess(ctx); err != nil {
		return Event{}, err
	}
	return s.repository.Read(ctx, id)
}

func (s *HistoryService) List(ctx context.Context, q Query) ([]EventSummary, bool, int64, int64, error) {
	if _, err := readAccess(ctx); err != nil {
		return nil, false, 0, 0, err
	}
	return s.repository.List(ctx, q)
}
