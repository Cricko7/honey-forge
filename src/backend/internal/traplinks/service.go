// Package traplinks owns directed raw-traffic links between organization traps.
package traplinks

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin/binding"

	"honey-forge/internal/contract"
)

type Link struct {
	SourceID string `json:"source_id" validate:"required,uuid"`
	TargetID string `json:"target_id" validate:"required,uuid,nefield=SourceID"`
}

type store interface {
	List(context.Context, string) ([]Link, error)
	Create(context.Context, string, Link) error
	Delete(context.Context, string, string, string) error
	Targets(context.Context, string, string) ([]string, error)
}

type Service struct{ store store }

func NewService(storage store) *Service { contract.Configure(); return &Service{storage} }

func organization(ctx context.Context, mutate bool) (string, error) {
	roles := []contract.Role{contract.Admin, contract.Viewer}
	if mutate {
		roles = []contract.Role{contract.Admin}
	}
	if err := contract.Authorize(ctx, roles...); err != nil {
		return "", err
	}
	p, _ := contract.PrincipalFrom(ctx)
	if !contract.ValidID(string(p.OrganizationID)) {
		return "", contract.NewError("unauthenticated")
	}
	return string(p.OrganizationID), ctx.Err()
}

func (s *Service) List(ctx context.Context) ([]Link, error) {
	org, err := organization(ctx, false)
	if err != nil {
		return nil, err
	}
	return s.store.List(ctx, org)
}

func (s *Service) Create(ctx context.Context, link Link) (Link, error) {
	org, err := organization(ctx, true)
	if err != nil {
		return Link{}, err
	}
	link.SourceID = strings.ToLower(link.SourceID)
	link.TargetID = strings.ToLower(link.TargetID)
	if err := binding.Validator.ValidateStruct(link); err != nil {
		return Link{}, contract.NewError("validation_failed")
	}
	if err := s.store.Create(ctx, org, link); err != nil {
		return Link{}, err
	}
	return link, nil
}

func (s *Service) Delete(ctx context.Context, source, target string) error {
	org, err := organization(ctx, true)
	if err != nil {
		return err
	}
	link := Link{strings.ToLower(source), strings.ToLower(target)}
	if err := binding.Validator.ValidateStruct(link); err != nil {
		return contract.NewError("validation_failed")
	}
	return s.store.Delete(ctx, org, link.SourceID, link.TargetID)
}

// Targets allows an agent to route only its own incoming traffic.
func (s *Service) Targets(ctx context.Context, org, source string) ([]string, error) {
	if err := contract.RequireAgentTrap(ctx, contract.ID(source)); err != nil {
		return nil, err
	}
	if err := contract.RequireOrganization(ctx, contract.ID(org)); err != nil {
		return nil, err
	}
	return s.store.Targets(ctx, org, source)
}
