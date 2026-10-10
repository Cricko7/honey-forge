package configschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/contract"
)

// SchemaStore retains the exact immutable versions used by all feature modules.
// Registry provides an optional process cache; PostgreSQL is authoritative.
type SchemaStore struct{ pool *pgxpool.Pool }

func NewSchemaStore(pool *pgxpool.Pool) *SchemaStore { return &SchemaStore{pool} }
func (s *SchemaStore) Register(ctx context.Context, typeID string, version contract.TypeVersion, raw json.RawMessage) error {
	if !contract.ValidTypeID(typeID) || version < 1 || version > contract.MaxRevision {
		return contract.NewError("validation_failed")
	}
	if _, err := Compile(raw); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO schema_versions(type_id,type_version,schema) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, typeID, version, []byte(raw))
	if err != nil {
		return fmt.Errorf("register schema version: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var same bool
	if err := s.pool.QueryRow(ctx, `SELECT schema=$3::jsonb FROM schema_versions WHERE type_id=$1 AND type_version=$2`, typeID, version, []byte(raw)).Scan(&same); err != nil {
		return fmt.Errorf("compare schema version: %w", err)
	}
	if !same {
		return &contract.Error{Code: "schema_version_conflict", Message: "Type version already has a different schema", Status: 409}
	}
	return nil
}

func (s *SchemaStore) Get(ctx context.Context, typeID string, version contract.TypeVersion) (*Schema, error) {
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT schema FROM schema_versions WHERE type_id=$1 AND type_version=$2`, typeID, version).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, contract.NewError("resource_not_found")
		}
		return nil, fmt.Errorf("read schema version: %w", err)
	}
	return Compile(raw)
}
