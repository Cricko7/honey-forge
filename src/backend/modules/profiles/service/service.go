package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	profilecore "honey-forge/src/backend/modules/profiles"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"honey-forge/src/backend/modules/auth"
)

// store is the consumed test seam; transaction implementations own atomic writes.
type store interface {
	Create(context.Context, auth.AuthContext, string, [32]byte, func() (profilecore.Profile, error)) (profilecore.CreateResult, error)
	ReadProfile(context.Context, string, string) (profilecore.Profile, error)
	Update(context.Context, auth.AuthContext, string, func(profilecore.Profile) (profilecore.Profile, []string, error)) (profilecore.Profile, error)
	Delete(context.Context, auth.AuthContext, string, func(profilecore.Profile, pgx.Tx) error) error
	ReadSnapshot(context.Context, string, string, int32) (profilecore.Snapshot, error)
	List(context.Context, string, profilecore.ListQuery) ([]profilecore.Profile, int64, error)
}

type Service struct {
	store     store
	deps      profilecore.Dependencies
	cursorKey [32]byte
}

func NewService(storage store, deps profilecore.Dependencies) *Service {
	s := &Service{store: storage, deps: deps}
	if _, err := rand.Read(s.cursorKey[:]); err != nil {
		panic(err)
	}

	return s
}

func Authorize(a auth.AuthContext, write bool) error {
	if !profilecore.ValidID(a.UserID) || !profilecore.ValidID(a.OrganizationID) {
		return profilecore.ErrUnauthorized
	}

	if a.Role != auth.RoleAdmin && (write || a.Role != auth.RoleViewer) {
		return profilecore.ErrForbidden
	}

	return nil
}

func (s *Service) Create(ctx context.Context, a auth.AuthContext, req profilecore.CreateRequest) (profilecore.CreateResult, error) {
	if err := Authorize(a, true); err != nil {
		return profilecore.CreateResult{}, err
	}

	if err := profilecore.ValidateRequest(req); err != nil {
		return profilecore.CreateResult{}, err
	}

	if err := ctx.Err(); err != nil {
		return profilecore.CreateResult{}, err
	}

	req.Name = strings.TrimSpace(req.Name)
	req.RequestID = strings.ToLower(req.RequestID)
	encoded, err := profilecore.Canonical(req)
	if err != nil {
		return profilecore.CreateResult{}, fmt.Errorf("encoding profile request: %w", err)
	}

	if s.store == nil {
		return profilecore.CreateResult{}, profilecore.ErrDatabaseUnavailable
	}

	result, err := s.store.Create(ctx, a, req.RequestID, sha256.Sum256(encoded), func() (profilecore.Profile, error) {
		typ, err := s.lookup(ctx, req.TypeID, int32(req.TypeVersion))
		if err != nil {
			return profilecore.Profile{}, err
		}

		if !typ.AvailableForNewProfiles {
			return profilecore.Profile{}, profilecore.ErrTypeUnavailable
		}

		config := profilecore.CloneObject(req.Config)
		if err := profilecore.CheckConfig(ctx, typ, config); err != nil {
			return profilecore.Profile{}, err
		}

		now := time.Now().UTC().Truncate(time.Microsecond)
		return profilecore.Profile{
			ID: profilecore.NewID(), Name: req.Name, Description: req.Description,
			TypeID: req.TypeID, TypeVersion: int32(req.TypeVersion),
			InteractionLevel: typ.InteractionLevel, Config: config,
			SecretFieldsSet: profilecore.InstalledSecrets(typ, config), Revision: 1,
			CreatedAt: now, UpdatedAt: now, OrganizationID: a.OrganizationID,
		}, nil
	})
	if err != nil {
		return profilecore.CreateResult{}, fmt.Errorf("creating profile: %w", err)
	}

	result.Profile = profilecore.Redacted(result.Profile)
	return result, nil
}

func (s *Service) ReadProfile(ctx context.Context, a auth.AuthContext, id string) (profilecore.Profile, error) {
	if err := Authorize(a, false); err != nil {
		return profilecore.Profile{}, err
	}

	if s.store == nil {
		return profilecore.Profile{}, profilecore.ErrDatabaseUnavailable
	}

	p, err := s.store.ReadProfile(ctx, a.OrganizationID, strings.ToLower(id))
	if err != nil {
		return profilecore.Profile{}, fmt.Errorf("reading profile: %w", err)
	}

	return profilecore.Redacted(p), nil
}

func (s *Service) Patch(ctx context.Context, a auth.AuthContext, id, etag string, req profilecore.PatchRequest) (profilecore.Profile, error) {
	if err := Authorize(a, true); err != nil {
		return profilecore.Profile{}, err
	}

	if s.store == nil {
		return profilecore.Profile{}, profilecore.ErrDatabaseUnavailable
	}

	p, err := s.store.Update(ctx, a, strings.ToLower(id), func(p profilecore.Profile) (profilecore.Profile, []string, error) {
		if err := profilecore.Precondition(p, etag); err != nil {
			return profilecore.Profile{}, nil, err
		}

		if err := profilecore.ValidateRequest(req); err != nil {
			return profilecore.Profile{}, nil, err
		}

		if req.Name == nil && req.Description == nil && req.Config == nil && req.ClearSecretFields == nil {
			return profilecore.Profile{}, nil, profilecore.ErrValidation
		}

		fields := []string{}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if p.Name != name {
				p.Name = name
				fields = append(fields, "/name")
			}
		}

		if req.Description != nil && p.Description != *req.Description {
			p.Description = *req.Description
			fields = append(fields, "/description")
		}

		if req.Config != nil || len(req.ClearSecretFields) > 0 {
			typ, err := s.lookup(ctx, p.TypeID, p.TypeVersion)
			if err != nil {
				return profilecore.Profile{}, nil, err
			}

			config, err := profilecore.PatchConfig(typ, p, req)
			if err != nil {
				return profilecore.Profile{}, nil, err
			}

			if err := profilecore.CheckConfig(ctx, typ, config); err != nil {
				return profilecore.Profile{}, nil, err
			}

			if !profilecore.EqualJSON(config, p.Config) {
				p.Config = config
				p.SecretFieldsSet = profilecore.InstalledSecrets(typ, config)
				fields = append(fields, "/config")
			}
		}

		if len(fields) > 0 {
			if p.Revision == math.MaxInt32 {
				return profilecore.Profile{}, nil, profilecore.ErrRevisionExhausted
			}

			p.Revision++
			p.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
		}

		return p, fields, nil
	})
	if err != nil {
		return profilecore.Profile{}, fmt.Errorf("patching profile: %w", err)
	}

	return profilecore.Redacted(p), nil
}

func (s *Service) Delete(ctx context.Context, a auth.AuthContext, id, etag string) error {
	if err := Authorize(a, true); err != nil {
		return err
	}

	if s.store == nil {
		return profilecore.ErrDatabaseUnavailable
	}

	err := s.store.Delete(ctx, a, strings.ToLower(id), func(p profilecore.Profile, tx pgx.Tx) error {
		if err := profilecore.Precondition(p, etag); err != nil {
			return err
		}

		if s.deps.HasLiveBindings == nil {
			return profilecore.ErrUnavailable
		}

		used, err := s.deps.HasLiveBindings(ctx, a.OrganizationID, p.ID, tx)
		if err != nil {
			return fmt.Errorf("checking profile bindings: %w", err)
		}

		if used {
			return profilecore.ErrInUse
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("deleting profile: %w", err)
	}

	return nil
}

func (s *Service) ReadSnapshot(ctx context.Context, org, id string, revision int32) (profilecore.Snapshot, error) {
	if s.store == nil {
		return profilecore.Snapshot{}, profilecore.ErrDatabaseUnavailable
	}

	return s.store.ReadSnapshot(ctx, org, id, revision)
}

func (s *Service) lookup(ctx context.Context, id string, version int32) (profilecore.Type, error) {
	if s.deps.LookupType == nil {
		return profilecore.Type{}, profilecore.ErrUnavailable
	}

	typ, err := s.deps.LookupType(ctx, id, version)
	if err != nil {
		return profilecore.Type{}, fmt.Errorf("looking up profile type: %w", err)
	}

	return typ, nil
}
