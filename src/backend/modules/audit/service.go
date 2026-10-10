package audit

import (
	"context"
	"honey-forge/internal/contract"
)

type store interface {
	Read(context.Context, string, string) (Entry, error)
	List(context.Context, string, Query) ([]Entry, bool, int64, error)
}
type Service struct{ store store }

func NewService(s store) *Service { return &Service{store: s} }
func access(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := contract.Authorize(ctx, contract.Admin, contract.Viewer); err != nil {
		return "", err
	}
	p, _ := contract.PrincipalFrom(ctx)
	if !contract.ValidID(string(p.OrganizationID)) {
		return "", contract.NewError("unauthenticated")
	}
	return string(p.OrganizationID), nil
}
func (s *Service) Read(ctx context.Context, id string) (Entry, error) {
	org, err := access(ctx)
	if err != nil {
		return Entry{}, err
	}
	return s.store.Read(ctx, org, id)
}
func (s *Service) List(ctx context.Context, q Query) ([]Entry, bool, int64, error) {
	org, err := access(ctx)
	if err != nil {
		return nil, false, 0, err
	}
	return s.store.List(ctx, org, q)
}
