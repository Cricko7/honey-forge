package configschema

import (
	"encoding/json"
	"honey-forge/internal/contract"
	"honey-forge/internal/postgres"
	"os"
	"testing"
)

func TestImmutableVersionsAcrossRestart(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	pool, err := postgres.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := postgres.Migrate(t.Context(), pool, os.DirFS("../../../../migrations")); err != nil {
		t.Fatal(err)
	}
	name := "t" + string(contract.NewID())
	store := NewSchemaStore(pool)
	if err := store.Register(t.Context(), name, 1, json.RawMessage(`{"type":"object"}`)); err != nil {
		t.Fatal(err)
	}
	restarted := NewSchemaStore(pool)
	if err := restarted.Register(t.Context(), name, 1, json.RawMessage(`{"type":"string"}`)); err == nil {
		t.Fatal("changed immutable schema")
	}
	schema, err := restarted.Get(t.Context(), name, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(json.RawMessage(`{}`), "/config"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Get(t.Context(), name, 2); err == nil {
		t.Fatal("unknown version accepted")
	}
}
