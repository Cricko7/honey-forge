//go:build integration

package repository

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
	"honey-forge/internal/postgres"
	"honey-forge/modules/commands"
)

const (
	testOrg  = "11111111-1111-4111-8111-111111111111"
	testUser = "22222222-2222-4222-8222-222222222222"
	testTrap = "33333333-3333-4333-8333-333333333333"
)

type testTraps struct{ pool *pgxpool.Pool }

func (t testTraps) Lock(ctx context.Context, tx pgx.Tx, org, id string) (commands.Trap, error) {
	var trap commands.Trap
	err := tx.QueryRow(ctx, `SELECT id::text,organization_id::text,COALESCE(profile_id::text,''),type_id,type_version,applied_revision,active_command_id::text FROM test_traps WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, org, id).Scan(&trap.ID, &trap.OrganizationID, &trap.ProfileID, &trap.TypeID, &trap.TypeVersion, &trap.AppliedProfileRevision, &trap.ActiveCommandID)
	if errors.Is(err, pgx.ErrNoRows) {
		return commands.Trap{}, commands.ErrNotFound
	}

	return trap, err
}

func (t testTraps) Activate(ctx context.Context, tx pgx.Tx, trap commands.Trap, command commands.Command) (int64, error) {
	var version int64
	err := tx.QueryRow(ctx, `UPDATE test_traps SET active_command_id=$2,state_version=state_version+1 WHERE id=$1 RETURNING state_version`, trap.ID, command.ID).Scan(&version)
	return version, err
}

func (t testTraps) Release(ctx context.Context, tx pgx.Tx, trap commands.Trap, _ commands.Command) (int64, error) {
	var version int64
	err := tx.QueryRow(ctx, `UPDATE test_traps SET active_command_id=NULL,state_version=state_version+1 WHERE id=$1 RETURNING state_version`, trap.ID).Scan(&version)
	return version, err
}

func (t testTraps) Complete(ctx context.Context, tx pgx.Tx, trap commands.Trap, command commands.Command, runtime commands.AgentRuntime) (int64, error) {
	return t.Release(ctx, tx, trap, command)
}

func (t testTraps) Visible(ctx context.Context, org, id string) (bool, error) {
	var visible bool
	err := t.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM test_traps WHERE organization_id=$1 AND id=$2)`, org, id).Scan(&visible)
	return visible, err
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}

	admin, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	schema := "commands_" + strings.ReplaceAll(string(contract.NewID()), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()

	pool, err := postgres.Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if err := postgres.Migrate(t.Context(), pool, os.DirFS("../../../../../migrations")); err != nil {
		t.Fatal(err)
	}

	return pool
}
