//go:build integration

package auth

import (
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPostgresMigrationPreservesExistingUsers(t *testing.T) {
	pool := integrationPool(t)
	applyMigrations(t, pool, "Down")
	dir := filepath.Join("..", "..", "migrations")
	applyMigration(t, pool, filepath.Join(dir, "20261008000100_auth.sql"), "Up")

	orgID, userID := newUUID(), newUUID()
	hash, err := bcrypt.GenerateFromPassword([]byte("demo-password-2026"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO organizations (id, name) VALUES ($1, 'Legacy org')", orgID); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), "INSERT INTO users (id, organization_id, email, password_hash) VALUES ($1, $2, 'legacy@example.com', $3)", userID, orgID, string(hash))
	if err != nil {
		t.Fatal(err)
	}

	applyMigration(t, pool, filepath.Join(dir, "20261009000100_operator_sessions.sql"), "Up")
	service := NewService(NewRepository(pool))
	view, raw, err := service.Login(t.Context(), LoginRequest{"legacy@example.com", "demo-password-2026"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.User.ID != userID || view.User.OrganizationID != orgID || view.User.Role != RoleAdmin || view.Organization.Name != "Legacy org" {
		t.Fatal("migration lost the old identity")
	}
	session, err := service.ResolveSession(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	code, err := service.JoinCode(t.Context(), session.AuthContext())
	if err != nil || code.Revision != 1 || !validOpaque(code.Code, 32) {
		t.Fatal("legacy organization did not receive its join code")
	}
}
