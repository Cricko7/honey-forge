// Package enginepipe supervises bounded observations from isolated native engines.
package enginepipe

import (
	"context"
	"encoding/json"
	"fmt"

	"honey-forge/internal/configschema"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

type Config struct {
	Services []struct {
		Name     string `json:"name"`
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
	} `json:"services"`
	Session struct {
		MaxConnections int `json:"max_connections"`
		MaxSeconds     int `json:"max_seconds"`
	} `json:"session"`
}

func ParseConfig(ctx context.Context, snapshot profiles.Snapshot, typeID string) (Config, error) {
	var c Config
	if snapshot.TypeID != typeID || snapshot.TypeVersion != 1 || snapshot.ProfileRevision < 1 {
		return c, fmt.Errorf("unsupported engine snapshot")
	}
	if err := ctx.Err(); err != nil {
		return c, err
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return c, fmt.Errorf("encode engine configuration: %w", err)
	}
	for _, d := range catalog.BuiltinDefinitions() {
		if string(d.Entry.TypeID) != typeID || d.Entry.TypeVersion != 1 {
			continue
		}
		schema, err := configschema.Compile(d.Entry.ConfigSchema)
		if err != nil {
			return c, fmt.Errorf("compile engine schema: %w", err)
		}
		if err := schema.Validate(raw, ""); err != nil {
			return c, fmt.Errorf("validate engine configuration: %w", err)
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, fmt.Errorf("decode engine configuration: %w", err)
		}
		return c, nil
	}
	return c, fmt.Errorf("engine descriptor is missing")
}
