package traps

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/internal/postgres"
	"honey-forge/modules/agentws"
	"honey-forge/modules/commands"
)

// Ingester is module 07's persistence boundary, not a positive-ack queue.
type Ingester interface {
	Ingest(context.Context, agentws.Identity, string, agentws.TelemetryBatch) (agentws.TelemetryAck, error)
}
type Gateway struct {
	repository *Repository
	catalog    typeCatalog
	ingester   Ingester
	now        func() time.Time
}

func NewGateway(repository *Repository, cat typeCatalog, ingester Ingester) *Gateway {
	return &Gateway{repository, cat, ingester, func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }}
}
func (g *Gateway) Authenticate(ctx context.Context, token string) (agentws.Identity, error) {
	secret, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(secret) != 32 {
		return agentws.Identity{}, contract.NewError("agent_unauthenticated")
	}
	hash := sha256.Sum256([]byte(token))
	var identity agentws.Identity
	err = g.repository.pool.QueryRow(ctx, `SELECT organization_id::text,id::text,type_id,type_version,generation FROM traps WHERE token_hash=$1 AND deleted_at IS NULL`, hash[:]).Scan(&identity.OrganizationID, &identity.TrapID, &identity.TypeID, &identity.TypeVersion, &identity.CredentialGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity, contract.NewError("agent_unauthenticated")
	}
	if err != nil {
		if postgres.IsUnavailable(err) {
			return identity, fmt.Errorf("authenticate agent: %w", errors.Join(contract.NewError("database_unavailable"), err))
		}
		return identity, fmt.Errorf("authenticate agent: %w", err)
	}
	entry, err := g.catalog.LookupType(ctx, identity.TypeID, contract.TypeVersion(identity.TypeVersion))
	if err != nil {
		return identity, err
	}
	for _, action := range entry.Actions {
		identity.RequiredActions = append(identity.RequiredActions, string(action.Action))
	}
	return identity, nil
}

// Hello pins the authenticated connection and the non-secret agent metadata.
func (g *Gateway) Hello(ctx context.Context, identity agentws.Identity, connection string, hello agentws.AgentHello) (agentws.State, error) {
	if len(hello.AgentVersion) < 1 || len(hello.AgentVersion) > 64 || len(hello.Hostname) < 1 || len(hello.Hostname) > 255 {
		return agentws.State{}, contract.NewError("validation_failed")
	}
	return g.observe(ctx, identity, connection, hello.Runtime, &hello, true)
}
func (g *Gateway) Observe(ctx context.Context, identity agentws.Identity, connection string, runtime commands.AgentRuntime) (agentws.State, error) {
	return g.observe(ctx, identity, connection, runtime, nil, true)
}
func (g *Gateway) Report(ctx context.Context, identity agentws.Identity, connection string, runtime commands.AgentRuntime) (agentws.State, error) {
	return g.observe(ctx, identity, connection, runtime, nil, false)
}
func (g *Gateway) observe(ctx context.Context, identity agentws.Identity, connection string, runtime commands.AgentRuntime, hello *agentws.AgentHello, refresh bool) (agentws.State, error) {
	var state agentws.State
	err := g.repository.mutations.AgentWrite(ctx, contract.ID(identity.TrapID), func(ctx context.Context, tx pgx.Tx) ([]mutation.Change, error) {
		r, err := lockRecord(ctx, tx, identity.OrganizationID, identity.TrapID)
		if err != nil {
			return nil, err
		}
		if r.Generation != identity.CredentialGeneration || len(r.TokenHash) == 0 {
			return nil, contract.NewError("agent_unauthenticated")
		}
		if hello == nil && (r.ConnectionID == nil || *r.ConnectionID != connection) {
			return nil, contract.NewError("agent_unauthenticated")
		}
		before := r.Trap
		if hello != nil {
			r.Agent = &AgentStatus{AgentVersion: hello.AgentVersion, Hostname: hello.Hostname}
		}
		if err := applyRuntime(ctx, tx, &r, runtime); err != nil {
			return nil, err
		}
		if refresh {
			now := g.now()
			r.LastSeenAt = &now
			r.Connectivity = "online"
			r.ConnectionID = &connection
		}
		state = agentState(r)
		if reflect.DeepEqual(before, r.Trap) {
			return nil, errUnchanged
		}
		version, err := nextVersion(r.StateVersion)
		if err != nil {
			return nil, err
		}
		r.StateVersion = version
		if err := save(ctx, tx, r); err != nil {
			return nil, err
		}
		return []mutation.Change{change(r, "trap.changed")}, nil
	})
	if errors.Is(err, errUnchanged) {
		err = nil
	}
	return state, err
}
func applyRuntime(ctx context.Context, tx pgx.Tx, r *Record, runtime commands.AgentRuntime) error {
	if runtime.BufferedEvents < 0 || runtime.BufferBytes < 0 || runtime.BufferCapacityBytes <= 0 {
		return contract.NewError("validation_failed")
	}
	switch runtime.RuntimeState {
	case "running", "stopped", "error", "unknown":
	default:
		return contract.NewError("validation_failed")
	}
	switch runtime.BufferState {
	case "ok", "full", "unavailable":
	default:
		return contract.NewError("validation_failed")
	}
	if runtime.RuntimeState == "error" && runtime.LastError == nil {
		return contract.NewError("validation_failed")
	}
	if runtime.LastError != nil {
		copy := *runtime.LastError
		if err := commands.NormalizeRuntimeError(&copy); err != nil {
			return contract.NewError("validation_failed")
		}
		runtime.LastError = &copy
	}
	if runtime.AppliedProfileRevision != nil {
		var known bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM commands WHERE trap_id=$1 AND action='apply_config' AND target_profile_revision=$2 AND started_at IS NOT NULL)`, r.ID, *runtime.AppliedProfileRevision).Scan(&known); err != nil {
			return fmt.Errorf("check issued snapshot: %w", err)
		}
		if !known {
			return contract.NewError("unknown_configuration")
		}
	}
	if r.Agent != nil {
		agent := *r.Agent
		agent.BufferedEvents, agent.BufferBytes, agent.BufferCapacityBytes, agent.BufferState, agent.LastError = runtime.BufferedEvents, runtime.BufferBytes, runtime.BufferCapacityBytes, runtime.BufferState, runtime.LastError
		r.Agent = &agent
	}
	r.RuntimeState = runtime.RuntimeState
	r.AppliedProfileRevision = runtime.AppliedProfileRevision
	return nil
}
func agentState(r Record) agentws.State {
	state := agentws.State{CurrentConfiguration: r.AppliedConfiguration, DesiredProfileRevision: r.DesiredProfileRevision, DesiredState: r.DesiredState, HeartbeatIntervalSeconds: 10}
	if r.AppliedConfiguration != nil {
		raw, err := json.Marshal(r.AppliedConfiguration.Config)
		if err == nil {
			var config struct {
				Management struct {
					Interval int `json:"heartbeat_interval_seconds"`
				} `json:"management"`
			}
			if json.Unmarshal(raw, &config) == nil && config.Management.Interval >= 5 && config.Management.Interval <= 10 {
				state.HeartbeatIntervalSeconds = config.Management.Interval
			}
		}
	}
	return state
}
func (g *Gateway) Offline(ctx context.Context, identity agentws.Identity, connection string) error {
	return g.offline(ctx, identity, connection, nil)
}
func (g *Gateway) expireConnection(ctx context.Context, identity agentws.Identity, connection string, now time.Time) error {
	return g.offline(ctx, identity, connection, &now)
}
func (g *Gateway) offline(ctx context.Context, identity agentws.Identity, connection string, now *time.Time) error {
	err := g.repository.mutations.AgentWrite(ctx, contract.ID(identity.TrapID), func(ctx context.Context, tx pgx.Tx) ([]mutation.Change, error) {
		r, err := lockRecord(ctx, tx, identity.OrganizationID, identity.TrapID)
		var api *contract.Error
		if errors.As(err, &api) && api.Code == "resource_not_found" {
			return nil, errUnchanged
		}
		if err != nil {
			return nil, err
		}
		if r.ConnectionID == nil || *r.ConnectionID != connection {
			return nil, errUnchanged
		}
		if now != nil && online(r, *now) {
			return nil, errUnchanged
		}
		if err := disconnect(&r); err != nil {
			return nil, err
		}
		if err := save(ctx, tx, r); err != nil {
			return nil, err
		}
		return []mutation.Change{change(r, "trap.changed")}, nil
	})
	if errors.Is(err, errUnchanged) {
		return nil
	}
	return err
}
