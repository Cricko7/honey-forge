package redistrap

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
		Password string `json:"password"`
	} `json:"services"`
	Management struct {
		Heartbeat int `json:"heartbeat_interval_seconds"`
		Flush     int `json:"telemetry_flush_interval_ms"`
	} `json:"management"`
}

func ParseConfig(ctx context.Context, snapshot profiles.Snapshot) (Config, error) {
	var config Config
	if snapshot.TypeID != "redis-emulator" || snapshot.TypeVersion != 1 || snapshot.ProfileRevision < 1 {
		return config, fmt.Errorf("unsupported trap snapshot")
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return config, fmt.Errorf("encode configuration: %w", err)
	}
	var descriptor json.RawMessage
	for _, definition := range catalog.BuiltinDefinitions() {
		if definition.Entry.TypeID == "redis-emulator" && definition.Entry.TypeVersion == 1 {
			descriptor = definition.Entry.ConfigSchema
			break
		}
	}
	schema, err := configschema.Compile(descriptor)
	if err != nil {
		return config, fmt.Errorf("compile Redis schema: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return config, err
	}
	if err := schema.Validate(raw, ""); err != nil {
		return config, fmt.Errorf("validate configuration: %w", err)
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return config, fmt.Errorf("decode configuration: %w", err)
	}
	return config, nil
}
