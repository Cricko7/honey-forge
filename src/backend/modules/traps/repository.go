package traps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
)

type Repository struct {
	pool      *pgxpool.Pool
	mutations *mutation.Store
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool, mutation.NewStore(pool)} }

const columns = `id::text,organization_id::text,profile_id::text,type_id,type_version,interaction_level,name,description,revision,state_version,created_at,updated_at,connectivity,last_seen_at,runtime_state,desired_state,desired_profile_revision,applied_profile_revision,agent,active_command_id::text,generation,token_hash,issued_at,connection_id::text,applied_configuration,(SELECT count(*) FROM trap_ingestions i WHERE i.trap_id=traps.id AND NOT i.finished)`

func scan(row pgx.Row) (Record, error) {
	var r Record
	var agent, configuration []byte
	err := row.Scan(&r.ID, &r.OrganizationID, &r.ProfileID, &r.TypeID, &r.TypeVersion, &r.InteractionLevel, &r.Name, &r.Description, &r.Revision, &r.StateVersion, &r.CreatedAt, &r.UpdatedAt, &r.Connectivity, &r.LastSeenAt, &r.RuntimeState, &r.DesiredState, &r.DesiredProfileRevision, &r.AppliedProfileRevision, &agent, &r.ActiveCommandID, &r.Generation, &r.TokenHash, &r.IssuedAt, &r.ConnectionID, &configuration, &r.PendingIngestions)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, contract.NewError("resource_not_found")
	}
	if err != nil {
		return r, fmt.Errorf("scan trap: %w", err)
	}
	if len(agent) > 0 {
		if err := json.Unmarshal(agent, &r.Agent); err != nil {
			return r, fmt.Errorf("decode agent: %w", err)
		}
	}
	if len(configuration) > 0 {
		if err := json.Unmarshal(configuration, &r.AppliedConfiguration); err != nil {
			return r, fmt.Errorf("decode applied configuration: %w", err)
		}
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	if r.LastSeenAt != nil {
		v := r.LastSeenAt.UTC()
		r.LastSeenAt = &v
	}
	if r.IssuedAt != nil {
		v := r.IssuedAt.UTC()
		r.IssuedAt = &v
	}
	return r, nil
}
func lockRecord(ctx context.Context, tx pgx.Tx, org, id string) (Record, error) {
	// Reservations can commit while this row lock is waiting. Read the count in
	// a new READ COMMITTED statement after the lock, rather than from the
	// snapshot captured before waiting.
	var locked string
	err := tx.QueryRow(ctx, `SELECT id::text FROM traps WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, org, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, contract.NewError("resource_not_found")
	}
	if err != nil {
		return Record{}, fmt.Errorf("lock trap: %w", err)
	}
	return scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM traps WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL`, org, id))
}
func (r *Repository) Read(ctx context.Context, org, id string) (Record, error) {
	if err := readAccess(ctx, org); err != nil {
		return Record{}, err
	}
	return scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM traps WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL`, org, id))
}
func readAccess(ctx context.Context, org string) error {
	if _, err := access(ctx, false); err != nil {
		return err
	}
	return contract.RequireOrganization(ctx, contract.ID(org))
}
func save(ctx context.Context, tx pgx.Tx, r Record) error {
	var agent, configuration any
	if r.Agent != nil {
		b, err := json.Marshal(r.Agent)
		if err != nil {
			return fmt.Errorf("encode agent: %w", err)
		}
		agent = string(b)
	}
	if r.AppliedConfiguration != nil {
		b, err := json.Marshal(r.AppliedConfiguration)
		if err != nil {
			return fmt.Errorf("encode configuration: %w", err)
		}
		configuration = string(b)
	}
	_, err := tx.Exec(ctx, `UPDATE traps SET name=$3,description=$4,revision=$5,state_version=$6,updated_at=$7,connectivity=$8,last_seen_at=$9,runtime_state=$10,desired_state=$11,desired_profile_revision=$12,applied_profile_revision=$13,agent=$14::jsonb,active_command_id=$15,generation=$16,token_hash=$17,issued_at=$18,connection_id=$19,applied_configuration=$20::jsonb WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL`, r.OrganizationID, r.ID, r.Name, r.Description, r.Revision, r.StateVersion, r.UpdatedAt, r.Connectivity, r.LastSeenAt, r.RuntimeState, r.DesiredState, r.DesiredProfileRevision, r.AppliedProfileRevision, agent, r.ActiveCommandID, r.Generation, r.TokenHash, r.IssuedAt, r.ConnectionID, configuration)
	if err != nil {
		return fmt.Errorf("save trap: %w", err)
	}
	return nil
}
func change(r Record, typ string) mutation.Change {
	return mutation.Change{Type: typ, ResourceID: contract.ID(r.ID), Metadata: mutation.Metadata{StateVersion: &r.StateVersion}}
}
func outcome(r Record, action string) mutation.Outcome {
	typ := "trap.changed"
	if action == "trap.deleted" {
		typ = "trap.deleted"
	}
	revision := contract.Revision(r.Revision)
	return mutation.Outcome{ResourceID: contract.ID(r.ID), Location: "/api/traps/" + r.ID, Action: action, Metadata: mutation.Metadata{Revision: &revision}, Changes: []mutation.Change{change(r, typ)}}
}

// Returning this sentinel rolls back a no-op without audit or notifications.
var errUnchanged = errors.New("trap unchanged")

func (r *Repository) Update(ctx context.Context, org, id string, update func(*Record) (string, error)) (Record, error) {
	var result Record
	err := r.mutations.Write(ctx, contract.ID(org), func(ctx context.Context, tx pgx.Tx) (mutation.Outcome, error) {
		current, err := lockRecord(ctx, tx, org, id)
		if err != nil {
			return mutation.Outcome{}, err
		}
		action, err := update(&current)
		if err != nil {
			return mutation.Outcome{}, err
		}
		result = current
		if action == "" {
			return mutation.Outcome{}, errUnchanged
		}
		if err := save(ctx, tx, current); err != nil {
			return mutation.Outcome{}, err
		}
		return outcome(current, action), nil
	})
	if errors.Is(err, errUnchanged) {
		err = nil
	}
	return result, err
}
