//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"honey-forge/internal/contract"
	"honey-forge/modules/auth"
	"honey-forge/modules/commands"
	"honey-forge/modules/commands/service"
)

func TestPostgresCommandLifecycle(t *testing.T) {
	pool := integrationPool(t)
	ctx := t.Context()
	for _, query := range []string{
		`INSERT INTO organizations(id,name,join_code) VALUES('` + testOrg + `','Demo',repeat('A',32))`,
		`INSERT INTO users(id,organization_id,email,password_hash,role) VALUES('` + testUser + `','` + testOrg + `','admin@example.com','hash','admin')`,
		`CREATE TABLE test_traps(id uuid PRIMARY KEY,organization_id uuid NOT NULL,profile_id uuid,type_id text NOT NULL,type_version integer NOT NULL,applied_revision integer,active_command_id uuid,state_version bigint NOT NULL DEFAULT 1,deleted_at timestamptz)`,
		`INSERT INTO test_traps(id,organization_id,type_id,type_version) VALUES('` + testTrap + `','` + testOrg + `','tcp-banner',1)`,
	} {
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}

	repo := New(pool, testTraps{pool})
	operator := service.New(repo, func(_ context.Context, _ string, _ int32, action string, params json.RawMessage) error {
		if action != "stop" || string(params) != `{}` {
			return commands.ErrInvalidParams
		}
		return nil
	})
	actor := auth.AuthContext{UserID: testUser, OrganizationID: testOrg, Role: auth.RoleAdmin}
	first, err := operator.Create(ctx, actor, testTrap, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "stop", Params: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}

	if first.Command.Status != commands.Queued {
		t.Fatalf("create status %s", first.Command.Status)
	}

	replay, err := operator.Create(ctx, actor, testTrap, commands.CreateRequest{RequestID: first.Command.RequestID, Action: "stop", Params: json.RawMessage(`{}`)})
	if err != nil || !replay.Replayed || replay.Command.ID != first.Command.ID {
		t.Fatalf("replay %+v: %v", replay, err)
	}
	if _, err := operator.Create(ctx, actor, testTrap, commands.CreateRequest{RequestID: first.Command.RequestID, Action: "start", Params: json.RawMessage(`{}`)}); err == nil || !strings.Contains(err.Error(), "idempotency_conflict") {
		t.Fatalf("changed replay accepted: %v", err)
	}

	if _, err := operator.Create(ctx, actor, testTrap, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "stop", Params: json.RawMessage(`{}`)}); !errors.Is(err, commands.ErrInProgress) {
		t.Fatalf("active command: %v", err)
	}

	viewer := actor
	viewer.Role = auth.RoleViewer
	if _, err := operator.Read(ctx, viewer, testTrap, first.Command.ID); err != nil {
		t.Fatal(err)
	}
	items, more, err := operator.List(ctx, viewer, testTrap, commands.ListQuery{Limit: 1, Status: commands.Queued})
	if err != nil || more || len(items) != 1 || items[0].ID != first.Command.ID {
		t.Fatalf("queued page %+v, more=%t: %v", items, more, err)
	}

	if err := repo.ExpireDue(ctx, testOrg, testTrap, first.Command.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	expired, err := operator.Read(ctx, viewer, testTrap, first.Command.ID)
	if err != nil || expired.Status != commands.Expired || expired.Error == nil || expired.Error.Code != "command_expired" {
		t.Fatalf("expired command %+v: %v", expired, err)
	}

	type attempt struct {
		result commands.CreateResult
		err    error
	}
	attempts := make(chan attempt, 2)
	for range 2 {
		go func() {
			result, err := operator.Create(ctx, actor, testTrap, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "stop", Params: json.RawMessage(`{}`)})
			attempts <- attempt{result, err}
		}()
	}
	var second commands.CreateResult
	var accepted, blocked int
	for range 2 {
		outcome := <-attempts
		switch {
		case outcome.err == nil:
			accepted++
			second = outcome.result
		case errors.Is(outcome.err, commands.ErrInProgress):
			blocked++
		default:
			t.Fatalf("concurrent command: %v", outcome.err)
		}
	}
	if accepted != 1 || blocked != 1 {
		t.Fatalf("concurrent outcomes accepted=%d blocked=%d", accepted, blocked)
	}

	agentContext := contract.WithPrincipal(ctx, contract.Principal{OrganizationID: contract.ID(testOrg), TrapID: contract.ID(testTrap), Role: contract.Agent})
	dispatch, err := repo.Claim(agentContext, testOrg, testTrap, time.Now())
	if err != nil || dispatch == nil || dispatch.CommandID != second.Command.ID {
		t.Fatalf("claim %+v: %v", dispatch, err)
	}

	agent := service.NewAgent(repo, func(context.Context, string, int32, string, json.RawMessage, bool) error { return nil })
	if _, err := agent.ExtendLease(agentContext, testOrg, testTrap, second.Command.ID, dispatch.LeaseID); err != nil {
		t.Fatalf("extend lease: %v", err)
	}
	if _, err := agent.ExtendLease(agentContext, testOrg, testTrap, second.Command.ID, string(contract.NewID())); !errors.Is(err, commands.ErrStaleLease) {
		t.Fatalf("stale lease accepted: %v", err)
	}
	input := commands.AgentResult{
		CommandID: second.Command.ID, LeaseID: dispatch.LeaseID, Status: commands.Succeeded,
		Result:  json.RawMessage(`{"runtime_state":"stopped","applied_profile_revision":null}`),
		Runtime: commands.AgentRuntime{RuntimeState: "stopped", BufferState: "ok", BufferCapacityBytes: 1024},
	}
	stale := input
	stale.LeaseID = string(contract.NewID())
	if _, err := agent.RecordResult(agentContext, testOrg, testTrap, stale); !errors.Is(err, commands.ErrStaleLease) {
		t.Fatalf("stale result accepted: %v", err)
	}
	ack, err := agent.RecordResult(agentContext, testOrg, testTrap, input)
	if err != nil || ack.IsZero() {
		t.Fatalf("record result: %v", err)
	}

	if _, err := agent.RecordResult(agentContext, testOrg, testTrap, input); err != nil {
		t.Fatalf("repeat result: %v", err)
	}
	conflicting := input
	conflicting.Result = json.RawMessage(`{"runtime_state":"running","applied_profile_revision":null}`)
	if _, err := agent.RecordResult(agentContext, testOrg, testTrap, conflicting); !errors.Is(err, commands.ErrResultConflict) {
		t.Fatalf("changed terminal result accepted: %v", err)
	}

	completed, err := operator.Read(ctx, viewer, testTrap, second.Command.ID)
	if err != nil || completed.Status != commands.Succeeded {
		t.Fatalf("completed command %+v: %v", completed, err)
	}

	var active *string
	if err := pool.QueryRow(ctx, `SELECT active_command_id::text FROM test_traps WHERE id=$1`, testTrap).Scan(&active); err != nil || active != nil {
		t.Fatalf("active command after result: %v, %v", active, err)
	}

	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mutation_audit WHERE organization_id=$1 AND action='command.created'`, testOrg).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audit count %d: %v", audits, err)
	}

	profileID := "66666666-6666-4666-8666-666666666666"
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO profiles(id,organization_id,type_id,type_version,interaction_level,revision,created_at) VALUES($1,$2,'tcp-banner',1,'low',2,now())`, profileID, testOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO profile_revisions(profile_id,revision,name,description,config,secret_fields_set,updated_at) VALUES($1,2,'Demo','',$2::json,'["/credentials/password"]'::jsonb,now())`, profileID, `{"credentials":{"password":"private"}}`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE test_traps SET profile_id=$2 WHERE id=$1`, testTrap, profileID); err != nil {
		t.Fatal(err)
	}

	apply := service.New(repo, func(_ context.Context, _ string, _ int32, action string, _ json.RawMessage) error {
		if action != "apply_config" {
			return commands.ErrUnsupportedAction
		}
		return nil
	})
	configuration, err := apply.Create(ctx, actor, testTrap, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "apply_config", Params: json.RawMessage(`{"profile_revision":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	public, err := json.Marshal(configuration.Command)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "private") || strings.Contains(string(public), "configuration") {
		t.Fatalf("secret snapshot exposed to operator: %s", public)
	}
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT configuration::text FROM commands WHERE id=$1`, configuration.Command.ID).Scan(&snapshot); err != nil || !strings.Contains(snapshot, "private") {
		t.Fatalf("snapshot not pinned: %v", err)
	}
	if err := repo.ExpireDue(ctx, testOrg, testTrap, configuration.Command.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := apply.Create(ctx, actor, testTrap, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "apply_config", Params: json.RawMessage(`{"profile_revision":1}`)}); !errors.Is(err, commands.ErrProfileChanged) {
		t.Fatalf("stale profile revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE test_traps SET deleted_at=now() WHERE id=$1`, testTrap); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.Read(ctx, viewer, testTrap, first.Command.ID); err != nil {
		t.Fatalf("history after tombstone: %v", err)
	}
	if _, err := operator.Create(ctx, actor, testTrap, commands.CreateRequest{RequestID: string(contract.NewID()), Action: "stop", Params: json.RawMessage(`{}`)}); !errors.Is(err, commands.ErrNotFound) {
		t.Fatalf("command accepted after tombstone: %v", err)
	}
	foreign := viewer
	foreign.OrganizationID = "99999999-9999-4999-8999-999999999999"
	if _, err := operator.Read(ctx, foreign, testTrap, first.Command.ID); !errors.Is(err, commands.ErrNotFound) {
		t.Fatalf("foreign history exposed: %v", err)
	}
}
