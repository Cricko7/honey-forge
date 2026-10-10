package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	commandrepo "honey-forge/modules/commands/repository"
	commandservice "honey-forge/modules/commands/service"
	"honey-forge/modules/profiles"
	"honey-forge/modules/traps"
)

type PGStore struct {
	Pool  *pgxpool.Pool
	WSURL string
}

func (s PGStore) List(ctx context.Context) ([]Target, error) {
	rows, err := s.Pool.Query(ctx, `SELECT t.id::text,t.organization_id::text,t.auto_deploy_actor_id::text,t.generation,t.deleted_at IS NOT NULL,
		t.profile_id::text,t.type_id,t.type_version,p.revision,r.config::text,t.applied_configuration::text,
		t.applied_profile_revision,t.active_command_id::text,
		EXISTS(SELECT 1 FROM commands c WHERE c.trap_id=t.id AND c.action='start' AND c.status='succeeded')
		FROM traps t
		LEFT JOIN profiles p ON p.id=t.profile_id
		LEFT JOIN profile_revisions r ON r.profile_id=p.id AND r.revision=p.revision
		WHERE t.auto_deploy ORDER BY t.created_at,t.id`)
	if err != nil {
		return nil, fmt.Errorf("query managed traps: %w", err)
	}
	defer rows.Close()
	var targets []Target
	for rows.Next() {
		var target Target
		var profileID, typeID string
		var typeVersion int32
		var revision *int32
		var config, applied *string
		if err := rows.Scan(&target.ID, &target.OrganizationID, &target.ActorID, &target.Generation, &target.Deleted, &profileID, &typeID, &typeVersion, &revision, &config, &applied, &target.AppliedProfileRevision, &target.ActiveCommandID, &target.HasStarted); err != nil {
			return nil, fmt.Errorf("scan managed trap: %w", err)
		}
		if !target.Deleted {
			if applied != nil {
				if err := json.Unmarshal([]byte(*applied), &target.Snapshot); err != nil {
					return nil, fmt.Errorf("decode applied trap snapshot %s: %w", target.ID, err)
				}
			} else {
				if revision == nil || config == nil {
					return nil, fmt.Errorf("profile missing for trap %s", target.ID)
				}
				target.Snapshot = profiles.Snapshot{ProfileID: profileID, ProfileRevision: *revision, TypeID: typeID, TypeVersion: typeVersion}
				if err := json.Unmarshal([]byte(*config), &target.Snapshot.Config); err != nil {
					return nil, fmt.Errorf("decode trap profile %s: %w", target.ID, err)
				}
			}
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed traps: %w", err)
	}
	return targets, nil
}

func (s PGStore) Advance(ctx context.Context, target Target) error {
	action, params := nextCommand(target)
	if action == "" {
		return nil
	}
	service := commandservice.New(commandrepo.New(s.Pool, traps.NewRepository(s.Pool)), func(context.Context, string, int32, string, json.RawMessage) error { return nil })
	actor := auth.AuthContext{UserID: target.ActorID, OrganizationID: target.OrganizationID, Role: auth.RoleAdmin}
	_, err := service.Create(ctx, actor, target.ID, commands.CreateRequest{RequestID: string(contract.NewID()), Action: action, Params: params})
	if err != nil {
		return fmt.Errorf("queue %s command: %w", action, err)
	}
	return nil
}

func nextCommand(target Target) (string, json.RawMessage) {
	if target.Deleted || target.ActiveCommandID != nil || target.HasStarted {
		return "", nil
	}
	if target.AppliedProfileRevision == nil {
		return "apply_config", json.RawMessage(fmt.Sprintf(`{"profile_revision":%d}`, target.Snapshot.ProfileRevision))
	}
	return "start", json.RawMessage(`{}`)
}

func (s PGStore) Issue(ctx context.Context, target Target) (string, int64, error) {
	principal := contract.Principal{UserID: contract.ID(target.ActorID), OrganizationID: contract.ID(target.OrganizationID), Role: contract.Admin}
	ctx = contract.WithPrincipal(ctx, principal)
	service := traps.NewService(traps.NewRepository(s.Pool), nil, s.WSURL, nil)
	credentials, err := service.IssueManagedCredentials(ctx, target.ID, target.Generation)
	if err != nil {
		return "", 0, fmt.Errorf("issue managed credentials: %w", err)
	}
	return credentials.Token, credentials.Generation, nil
}

// ReconcileLocked allows only one orchestrator to mutate Swarm and credentials.
func ReconcileLocked(ctx context.Context, pool *pgxpool.Pool, controller Controller) (result error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin orchestration lock: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(rollbackCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			result = errors.Join(result, fmt.Errorf("release orchestration lock: %w", err))
		}
	}()
	const lockID int64 = 0x68666f726368
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, lockID).Scan(&locked); err != nil {
		return fmt.Errorf("acquire orchestration lock: %w", err)
	}
	if !locked {
		return nil
	}
	return controller.Reconcile(ctx)
}
