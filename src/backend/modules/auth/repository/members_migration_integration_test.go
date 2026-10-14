//go:build integration

package repository

import (
	"errors"
	"path/filepath"
	"testing"

	"honey-forge/modules/auth"
	authservice "honey-forge/modules/auth/service"
)

func TestPostgresCreatorBackfillDoesNotGuessAmbiguousOwnership(t *testing.T) {
	pool := integrationPool(t)
	s := authservice.NewService(NewRepository(pool))
	owners := make(map[string]auth.User)
	for _, name := range []string{"single", "known", "ambiguous"} {
		view, _, err := s.Register(t.Context(), createRequest(name+"@example.com"), "")
		if err != nil {
			t.Fatal(err)
		}
		owners[name] = view.User
		if name != "single" {
			_, err := pool.Exec(t.Context(), `INSERT INTO users (id, organization_id, email, password_hash, role)
				VALUES ($1, $2, $3, 'test-only-hash', 'admin')`, auth.NewUUID(), view.Organization.ID, "second-"+name+"@example.com")
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, name := range []string{"single", "ambiguous"} {
		if _, err := pool.Exec(t.Context(), `DELETE FROM auth_audit WHERE organization_id = $1 AND action = 'organization.created'`, owners[name].OrganizationID); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join("..", "..", "..", "..", "..", "migrations", "20261014000100_organization_admin.sql")
	applyMigration(t, pool, file, "Down")
	applyMigration(t, pool, file, "Up")
	for _, name := range []string{"single", "known", "ambiguous"} {
		t.Run(name, func(t *testing.T) {
			owner := owners[name]
			actor := auth.AuthContext{UserID: owner.ID, OrganizationID: owner.OrganizationID, Role: auth.RoleAdmin}
			page, err := s.Members(t.Context(), actor, 50, "")
			if err != nil {
				t.Fatal(err)
			}
			if name == "ambiguous" {
				if page.Creator != nil {
					t.Fatal("ambiguous ownership was guessed")
				}
				if err := s.SetTrapPermission(t.Context(), actor, owner.ID, true, 1); !errors.Is(err, auth.ErrForbidden) {
					t.Fatalf("unknown creator delegated: %v", err)
				}
			} else if page.Creator == nil || page.Creator.ID != owner.ID {
				t.Fatal("known creator was not preserved")
			}
		})
	}
}
