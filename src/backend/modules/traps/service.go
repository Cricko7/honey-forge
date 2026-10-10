package traps

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin/binding"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

type store interface {
	Create(context.Context, string, CreateRequest, func(context.Context, profiles.Profile) error) (CreateResult, error)
	Read(context.Context, string, string) (Record, error)
	Update(context.Context, string, string, func(*Record) (string, error)) (Record, error)
	Delete(context.Context, string, string, func(Record) error) error
	List(context.Context, string, ListQuery) ([]Trap, bool, error)
}
type typeCatalog interface {
	LookupType(context.Context, string, contract.TypeVersion) (catalog.CatalogEntry, error)
}
type Service struct {
	store   store
	catalog typeCatalog
	wsURL   string
	revoke  func(string, int64)
	now     func() time.Time
}

func NewService(storage store, cat typeCatalog, wsURL string, revoke func(string, int64)) *Service {
	contract.Configure()
	return &Service{storage, cat, wsURL, revoke, func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }}
}
func access(ctx context.Context, admin bool) (string, error) {
	roles := []contract.Role{contract.Admin, contract.Viewer}
	if admin {
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
func validate(req any) error {
	if err := binding.Validator.ValidateStruct(req); err != nil {
		return contract.NewError("validation_failed")
	}
	return nil
}
func (s *Service) Create(ctx context.Context, req CreateRequest) (CreateResult, error) {
	org, err := access(ctx, true)
	if err != nil {
		return CreateResult{}, err
	}
	if err := validate(req); err != nil {
		return CreateResult{}, err
	}
	if s.store == nil || s.catalog == nil {
		return CreateResult{}, contract.NewError("service_unavailable")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.RequestID = strings.ToLower(req.RequestID)
	req.ProfileID = strings.ToLower(req.ProfileID)
	return s.store.Create(ctx, org, req, func(ctx context.Context, p profiles.Profile) error {
		entry, err := s.catalog.LookupType(ctx, p.TypeID, contract.TypeVersion(p.TypeVersion))
		if err != nil {
			return err
		}
		if !entry.AvailableForNewProfiles {
			return contract.NewError("trap_type_unavailable")
		}
		return nil
	})
}
func (s *Service) Read(ctx context.Context, id string) (Trap, error) {
	org, err := access(ctx, false)
	if err != nil {
		return Trap{}, err
	}
	if s.store == nil {
		return Trap{}, contract.NewError("service_unavailable")
	}
	r, err := s.store.Read(ctx, org, id)
	return r.Trap, err
}
func (s *Service) Patch(ctx context.Context, id string, expected int64, req PatchRequest) (Trap, error) {
	org, err := access(ctx, true)
	if err != nil {
		return Trap{}, err
	}
	if err := validate(req); err != nil {
		return Trap{}, err
	}
	if s.store == nil {
		return Trap{}, contract.NewError("service_unavailable")
	}
	r, err := s.store.Update(ctx, org, id, func(r *Record) (string, error) {
		changed, err := patch(r, expected, req, s.now())
		if err != nil || !changed {
			return "", err
		}
		return "trap.updated", nil
	})
	return r.Trap, err
}
func (s *Service) Delete(ctx context.Context, id string, expected int64) error {
	org, err := access(ctx, true)
	if err != nil {
		return err
	}
	if s.store == nil {
		return contract.NewError("service_unavailable")
	}
	err = s.store.Delete(ctx, org, id, func(r Record) error { return safeDelete(r, expected, s.now()) })
	if err == nil && s.revoke != nil {
		s.revoke(id, 0)
	}
	return err
}
func (s *Service) Credentials(ctx context.Context, id string) (CredentialsStatus, error) {
	org, err := access(ctx, true)
	if err != nil {
		return CredentialsStatus{}, err
	}
	if s.store == nil {
		return CredentialsStatus{}, contract.NewError("service_unavailable")
	}
	r, err := s.store.Read(ctx, org, id)
	return CredentialsStatus{TrapID: id, Generation: r.Generation, Active: len(r.TokenHash) > 0, IssuedAt: r.IssuedAt}, err
}
func fmtCredentialsETag(id string, generation int64) string {
	return fmt.Sprintf(`"agent-credentials:%s:%d"`, id, generation)
}
func (s *Service) IssueCredentials(ctx context.Context, id string, expected int64) (AgentCredentials, error) {
	org, err := access(ctx, true)
	if err != nil {
		return AgentCredentials{}, err
	}
	if expected < 0 || expected > 2147483646 {
		return AgentCredentials{}, contract.NewError("validation_failed")
	}
	if s.store == nil {
		return AgentCredentials{}, contract.NewError("service_unavailable")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return AgentCredentials{}, fmt.Errorf("generate agent token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	r, err := s.store.Update(ctx, org, id, func(r *Record) (string, error) {
		if r.Generation != expected {
			return "", contract.NewError("agent_credentials_changed")
		}
		generation, err := nextVersion(r.Generation)
		if err != nil {
			return "", err
		}
		if err := disconnect(r); err != nil {
			return "", err
		}
		r.Generation, r.TokenHash, r.IssuedAt = generation, hash[:], ptrTime(s.now())
		return "trap.agent_credentials_issued", nil
	})
	if err != nil {
		return AgentCredentials{}, err
	}
	if s.revoke != nil {
		s.revoke(id, r.Generation)
	}
	return AgentCredentials{TrapID: id, Token: token, Generation: r.Generation, AgentWSURL: s.wsURL, IssuedAt: *r.IssuedAt}, nil
}
func (s *Service) RevokeCredentials(ctx context.Context, id, etag string) error {
	org, err := access(ctx, true)
	if err != nil {
		return err
	}
	if s.store == nil {
		return contract.NewError("service_unavailable")
	}
	r, err := s.store.Update(ctx, org, id, func(r *Record) (string, error) {
		if etag == "" {
			return "", contract.NewError("precondition_required")
		}
		if etag != CredentialsETag(id, r.Generation) {
			return "", contract.NewError("agent_credentials_changed")
		}
		if len(r.TokenHash) == 0 {
			return "", nil
		}
		generation, err := nextVersion(r.Generation)
		if err != nil {
			return "", err
		}
		if err := disconnect(r); err != nil {
			return "", err
		}
		r.Generation, r.TokenHash = generation, nil
		return "trap.agent_credentials_revoked", nil
	})
	if err == nil && s.revoke != nil {
		s.revoke(id, r.Generation+1)
	}
	return err
}
func disconnect(r *Record) error {
	if r.Connectivity == "online" {
		version, err := nextVersion(r.StateVersion)
		if err != nil {
			return err
		}
		r.StateVersion = version
	}
	r.Connectivity = "offline"
	r.ConnectionID = nil
	return nil
}
func ptrTime(v time.Time) *time.Time { return &v }
func profileError(err error) error {
	if errors.Is(err, profiles.ErrNotFound) {
		return contract.NewError("resource_not_found")
	}
	return err
}
