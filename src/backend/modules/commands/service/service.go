package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
)

type store interface {
	Create(context.Context, string, string, string, json.RawMessage, func(context.Context, commands.Trap, commands.SnapshotReader) (commands.Prepared, error)) (commands.CreateResult, error)
	Read(context.Context, string, string, string) (commands.Command, error)
	List(context.Context, string, string, commands.ListQuery) ([]commands.Command, bool, error)
}

type CheckAction func(context.Context, string, int32, string, json.RawMessage) error

type Service struct {
	store       store
	checkAction CheckAction
}

func New(storage store, checkAction CheckAction) *Service {
	return &Service{store: storage, checkAction: checkAction}
}

func Authorize(a auth.AuthContext, write bool) error {
	if !contract.ValidID(a.UserID) || !contract.ValidID(a.OrganizationID) {
		return commands.ErrUnauthorized
	}

	if a.Role != auth.RoleAdmin && (write || a.Role != auth.RoleViewer) {
		return commands.ErrForbidden
	}

	return nil
}

func (s *Service) Create(ctx context.Context, a auth.AuthContext, trapID string, req commands.CreateRequest) (commands.CreateResult, error) {
	if err := Authorize(a, true); err != nil {
		return commands.CreateResult{}, err
	}

	if !contract.ValidID(trapID) || !contract.ValidID(req.RequestID) || req.Action == "" {
		return commands.CreateResult{}, commands.ErrValidation
	}

	var params map[string]json.RawMessage
	if contract.CheckJSON(req.Params) != nil || json.Unmarshal(req.Params, &params) != nil || params == nil {
		return commands.CreateResult{}, commands.ErrInvalidParams
	}

	if err := ctx.Err(); err != nil {
		return commands.CreateResult{}, err
	}

	if s.store == nil || s.checkAction == nil {
		return commands.CreateResult{}, commands.ErrUnavailable
	}

	requestID := strings.ToLower(req.RequestID)
	trapID = strings.ToLower(trapID)
	canonical, err := json.Marshal(struct {
		Action string                     `json:"action"`
		Params map[string]json.RawMessage `json:"params"`
	}{req.Action, params})
	if err != nil {
		return commands.CreateResult{}, fmt.Errorf("encode command request: %w", err)
	}

	ctx = operatorContext(ctx, a)
	result, err := s.store.Create(ctx, a.OrganizationID, trapID, requestID, canonical, func(ctx context.Context, trap commands.Trap, snapshots commands.SnapshotReader) (commands.Prepared, error) {
		if trap.OrganizationID != a.OrganizationID || trap.ID != trapID {
			return commands.Prepared{}, commands.ErrNotFound
		}

		if err := s.checkAction(ctx, trap.TypeID, trap.TypeVersion, req.Action, req.Params); err != nil {
			return commands.Prepared{}, err
		}

		prepared := commands.Prepared{Action: req.Action, Params: req.Params}
		switch req.Action {
		case "start":
			if trap.AppliedProfileRevision == nil {
				return commands.Prepared{}, commands.ErrConfigurationNotApplied
			}

		case "stop":
		case "apply_config":
			var input struct {
				Revision int32 `json:"profile_revision"`
			}
			if err := json.Unmarshal(req.Params, &input); err != nil || input.Revision < 1 {
				return commands.Prepared{}, commands.ErrInvalidParams
			}

			snapshot, err := snapshots.Current(ctx, a.OrganizationID, trap.ProfileID, input.Revision)
			if err != nil {
				if errors.Is(err, commands.ErrNotFound) {
					return commands.Prepared{}, commands.ErrProfileChanged
				}

				return commands.Prepared{}, fmt.Errorf("read command snapshot: %w", err)
			}

			if snapshot.ProfileRevision != input.Revision || snapshot.TypeID != trap.TypeID || snapshot.TypeVersion != trap.TypeVersion || snapshot.ProfileID != trap.ProfileID {
				return commands.Prepared{}, commands.ErrProfileChanged
			}

			prepared.TargetProfileRevision = &input.Revision
			prepared.Snapshot = &snapshot

		default:
			return commands.Prepared{}, commands.ErrUnsupportedAction
		}

		return prepared, nil
	})
	if err != nil {
		return commands.CreateResult{}, fmt.Errorf("create command: %w", err)
	}

	return result, nil
}

func (s *Service) Read(ctx context.Context, a auth.AuthContext, trapID, commandID string) (commands.Command, error) {
	if err := Authorize(a, false); err != nil {
		return commands.Command{}, err
	}

	if !contract.ValidID(trapID) || !contract.ValidID(commandID) {
		return commands.Command{}, commands.ErrValidation
	}
	if s.store == nil {
		return commands.Command{}, commands.ErrUnavailable
	}

	return s.store.Read(operatorContext(ctx, a), a.OrganizationID, strings.ToLower(trapID), strings.ToLower(commandID))
}

func (s *Service) List(ctx context.Context, a auth.AuthContext, trapID string, query commands.ListQuery) ([]commands.Command, bool, error) {
	if err := Authorize(a, false); err != nil {
		return nil, false, err
	}

	if !contract.ValidID(trapID) {
		return nil, false, commands.ErrValidation
	}

	if query.Limit < 1 || query.Limit > 100 {
		return nil, false, commands.ErrValidation
	}
	if s.store == nil {
		return nil, false, commands.ErrUnavailable
	}

	return s.store.List(operatorContext(ctx, a), a.OrganizationID, strings.ToLower(trapID), query)
}

func operatorContext(ctx context.Context, a auth.AuthContext) context.Context {
	return contract.WithPrincipal(ctx, contract.Principal{UserID: contract.ID(a.UserID), OrganizationID: contract.ID(a.OrganizationID), Role: contract.Role(a.Role)})
}
