package mutation

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
)

func TestAgentChanges(t *testing.T) {
	store, pool, ctx, scope := testStore(t)
	trap := contract.NewID()
	if _, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS test_agent_state(id uuid PRIMARY KEY,revision bigint NOT NULL,state_version bigint NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO test_agent_state VALUES($1,1,1)", string(trap)); err != nil {
		t.Fatal(err)
	}
	agent := contract.WithPrincipal(t.Context(), contract.Principal{Role: contract.Agent, OrganizationID: scope.OrganizationID, TrapID: trap})
	write := func(ctx context.Context, tx pgx.Tx) ([]Change, error) {
		var state int64
		if err := tx.QueryRow(ctx, "UPDATE test_agent_state SET state_version=state_version+1 WHERE id=$1 RETURNING state_version", string(trap)).Scan(&state); err != nil {
			return nil, err
		}
		return []Change{{Type: "trap.changed", ResourceID: trap, Metadata: Metadata{StateVersion: &state}}}, nil
	}
	if err := store.AgentWrite(agent, trap, write); err != nil {
		t.Fatal(err)
	}
	var revision, state int64
	if err := pool.QueryRow(ctx, "SELECT revision,state_version FROM test_agent_state WHERE id=$1", string(trap)).Scan(&revision, &state); err != nil || revision != 1 || state != 2 {
		t.Fatalf("versions %d %d %v", revision, state, err)
	}
	if err := store.AgentWrite(agent, contract.NewID(), write); err == nil {
		t.Fatal("foreign trap accepted")
	}
	if err := store.AgentWrite(ctx, trap, write); err == nil {
		t.Fatal("operator used agent mutation")
	}
}
