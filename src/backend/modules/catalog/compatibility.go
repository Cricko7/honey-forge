package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// BuiltinDefinitions selects a known historical descriptor only when that exact
// descriptor is already retained. Install still rejects all other mismatches.
func (r *Repository) BuiltinDefinitions(ctx context.Context) ([]Definition, error) {
	definitions := BuiltinDefinitions()
	legacy := LegacyHTTPPipelineDefinition()
	raw, err := json.Marshal(legacy.Entry)
	if err != nil {
		return nil, fmt.Errorf("encode historical HTTP descriptor: %w", err)
	}

	var retained bool
	err = r.pool.QueryRow(ctx, `SELECT entry=$1::jsonb FROM catalog_versions WHERE type_id='custom-http' AND type_version=1`, raw).Scan(&retained)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read historical HTTP descriptor: %w", err)
	}

	if retained {
		for i := range definitions {
			if definitions[i].Entry.TypeID == "custom-http" && definitions[i].Entry.TypeVersion == 1 {
				definitions[i] = legacy
			}
		}
	}

	return definitions, nil
}
