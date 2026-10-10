package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
	profilerepo "honey-forge/modules/profiles/repository"
)

// TrapBoundary is implemented by module 05. Every method receives the command transaction.
type TrapBoundary interface {
	Lock(context.Context, pgx.Tx, string, string) (commands.Trap, error)
	Activate(context.Context, pgx.Tx, commands.Trap, commands.Command) (int64, error)
	Release(context.Context, pgx.Tx, commands.Trap, commands.Command) (int64, error)
	Complete(context.Context, pgx.Tx, commands.Trap, commands.Command, commands.AgentRuntime) (int64, error)
	Visible(context.Context, string, string) (bool, error)
}

type Repository struct {
	pool      *pgxpool.Pool
	mutations *mutation.Store
	traps     TrapBoundary
}

func New(pool *pgxpool.Pool, traps TrapBoundary) *Repository {
	return &Repository{pool: pool, mutations: mutation.NewStore(pool), traps: traps}
}

type snapshotReader struct{ tx pgx.Tx }

func (s snapshotReader) Current(ctx context.Context, org, profileID string, revision int32) (profiles.Snapshot, error) {
	snapshot, err := profilerepo.CaptureProfile(ctx, s.tx, org, profileID, revision)
	if err != nil {
		if errors.Is(err, profiles.ErrProfileChanged) || errors.Is(err, profiles.ErrNotFound) {
			return profiles.Snapshot{}, commands.ErrProfileChanged
		}

		return profiles.Snapshot{}, fmt.Errorf("capture profile: %w", err)
	}

	return snapshot, nil
}

const columns = `id::text,trap_id::text,request_id::text,action,params::text,status,target_profile_revision,created_at,expires_at,started_at,finished_at,result::text,error::text`

func scan(row pgx.Row) (commands.Command, error) {
	var c commands.Command
	var params string
	var result, runtimeError *string
	err := row.Scan(&c.ID, &c.TrapID, &c.RequestID, &c.Action, &params, &c.Status, &c.TargetProfileRevision, &c.CreatedAt, &c.ExpiresAt, &c.StartedAt, &c.FinishedAt, &result, &runtimeError)
	if errors.Is(err, pgx.ErrNoRows) {
		return commands.Command{}, commands.ErrNotFound
	}

	if err != nil {
		return commands.Command{}, fmt.Errorf("scan command: %w", err)
	}

	c.Params = json.RawMessage(params)
	if result != nil {
		c.Result = json.RawMessage(*result)
	}

	if runtimeError != nil {
		if err := json.Unmarshal([]byte(*runtimeError), &c.Error); err != nil {
			return commands.Command{}, fmt.Errorf("decode command error: %w", err)
		}
	}

	c.CreatedAt = c.CreatedAt.UTC()
	c.ExpiresAt = c.ExpiresAt.UTC()
	return c, nil
}

func (r *Repository) Create(ctx context.Context, org, trapID, requestID string, normalized json.RawMessage, prepare func(context.Context, commands.Trap, commands.SnapshotReader) (commands.Prepared, error)) (commands.CreateResult, error) {
	if r.traps == nil {
		return commands.CreateResult{}, commands.ErrUnavailable
	}

	if err := contract.AuthorizeCapability(ctx, contract.WriteResources); err != nil {
		return commands.CreateResult{}, err
	}

	if err := contract.RequireOrganization(ctx, contract.ID(org)); err != nil {
		return commands.CreateResult{}, err
	}

	if err := r.ExpireDue(ctx, org, trapID, time.Now()); err != nil {
		return commands.CreateResult{}, fmt.Errorf("expire pending command: %w", err)
	}
	var created commands.Command
	scope := mutation.Scope{OrganizationID: contract.ID(org), Route: "/api/traps/:id/commands", TrapID: contract.ID(trapID), RequestID: contract.ID(requestID)}
	result, err := r.mutations.Create(ctx, scope, normalized, func(ctx context.Context, tx pgx.Tx) (mutation.Outcome, error) {
		trap, err := r.traps.Lock(ctx, tx, org, trapID)
		if err != nil {
			return mutation.Outcome{}, fmt.Errorf("lock trap: %w", err)
		}

		if trap.ActiveCommandID != nil {
			return mutation.Outcome{}, commands.ErrInProgress
		}

		prepared, err := prepare(ctx, trap, snapshotReader{tx})
		if err != nil {
			return mutation.Outcome{}, err
		}

		now := time.Now().UTC().Truncate(time.Microsecond)
		created = commands.Command{
			ID: string(contract.NewID()), TrapID: trapID, RequestID: requestID,
			Action: prepared.Action, Params: prepared.Params, Status: commands.Queued,
			TargetProfileRevision: prepared.TargetProfileRevision,
			CreatedAt:             now, ExpiresAt: now.Add(24 * time.Hour),
		}

		var configuration any
		if prepared.Snapshot != nil {
			encoded, encodeErr := json.Marshal(prepared.Snapshot)
			err = encodeErr
			if err != nil {
				return mutation.Outcome{}, fmt.Errorf("encode configuration snapshot: %w", err)
			}

			configuration = string(encoded)
		}

		_, err = tx.Exec(ctx, `INSERT INTO commands(id,organization_id,trap_id,request_id,action,params,status,target_profile_revision,configuration,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9::jsonb,$10,$11)`, created.ID, org, trapID, requestID, created.Action, string(created.Params), created.Status, created.TargetProfileRevision, configuration, created.CreatedAt, created.ExpiresAt)
		if err != nil {
			return mutation.Outcome{}, fmt.Errorf("insert command: %w", err)
		}

		stateVersion, err := r.traps.Activate(ctx, tx, trap, created)
		if err != nil {
			return mutation.Outcome{}, fmt.Errorf("activate command: %w", err)
		}

		return mutation.Outcome{
			ResourceID: contract.ID(created.ID),
			Location:   "/api/traps/" + trapID + "/commands/" + created.ID,
			Action:     "command.created",
			Changes: []mutation.Change{
				{Type: "command.changed", ResourceID: contract.ID(created.ID)},
				{Type: "trap.changed", ResourceID: contract.ID(trapID), Metadata: mutation.Metadata{StateVersion: &stateVersion}},
			},
		}, nil
	})
	if err != nil {
		return commands.CreateResult{}, fmt.Errorf("create command transaction: %w", err)
	}

	if result.Replayed {
		created, err = r.Read(ctx, org, trapID, string(result.ResourceID))
		if err != nil {
			return commands.CreateResult{}, err
		}
	}

	return commands.CreateResult{Command: created, Replayed: result.Replayed}, nil
}

func (r *Repository) Read(ctx context.Context, org, trapID, id string) (commands.Command, error) {
	if r.traps == nil {
		return commands.Command{}, commands.ErrUnavailable
	}
	if err := contract.AuthorizeCapability(ctx, contract.ReadResources); err != nil {
		return commands.Command{}, err
	}
	if err := contract.RequireOrganization(ctx, contract.ID(org)); err != nil {
		return commands.Command{}, err
	}

	visible, err := r.traps.Visible(ctx, org, trapID)
	if err != nil {
		return commands.Command{}, fmt.Errorf("check trap ownership: %w", err)
	}

	if !visible {
		return commands.Command{}, commands.ErrNotFound
	}

	return scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM commands WHERE organization_id=$1 AND trap_id=$2 AND id=$3`, org, trapID, id))
}

func (r *Repository) List(ctx context.Context, org, trapID string, query commands.ListQuery) ([]commands.Command, bool, error) {
	if r.traps == nil {
		return nil, false, commands.ErrUnavailable
	}
	if err := contract.AuthorizeCapability(ctx, contract.ReadResources); err != nil {
		return nil, false, err
	}
	if err := contract.RequireOrganization(ctx, contract.ID(org)); err != nil {
		return nil, false, err
	}

	visible, err := r.traps.Visible(ctx, org, trapID)
	if err != nil {
		return nil, false, fmt.Errorf("check trap ownership: %w", err)
	}

	if !visible {
		return nil, false, commands.ErrNotFound
	}

	rows, err := r.pool.Query(ctx, `SELECT `+columns+` FROM commands WHERE organization_id=$1 AND trap_id=$2 AND ($3::text='' OR status=$3) AND ($4::timestamptz IS NULL OR (created_at,id)<($4,$5::uuid)) ORDER BY created_at DESC,id DESC LIMIT $6`, org, trapID, string(query.Status), nullableTime(query.AfterTime), nullableID(query.AfterID), query.Limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list commands: %w", err)
	}
	defer rows.Close()

	items := []commands.Command{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, false, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate commands: %w", err)
	}

	more := len(items) > query.Limit
	if more {
		items = items[:query.Limit]
	}

	return items, more, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}

	return value
}

func nullableID(value string) any {
	if value == "" {
		return nil
	}

	return value
}
