package pythonhttp

import (
	"context"
	"encoding/json"
	"fmt"

	"honey-forge/internal/configschema"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

func Ports(ctx context.Context, snapshot profiles.Snapshot) ([]int, error) {
	if snapshot.TypeID != "custom-http" || (snapshot.TypeVersion < 1 || snapshot.TypeVersion > 3) || snapshot.ProfileRevision < 1 {
		return nil, fmt.Errorf("unsupported HTTP snapshot")
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return nil, fmt.Errorf("encode HTTP config: %w", err)
	}
	definitions := catalog.BuiltinDefinitions()
	if snapshot.TypeVersion == 1 {
		definitions = append(definitions, catalog.LegacyHTTPPipelineDefinition())
	}

	var validationErr error
	for _, definition := range definitions {
		if definition.Entry.TypeID != "custom-http" || int32(definition.Entry.TypeVersion) != snapshot.TypeVersion {
			continue
		}
		schema, err := configschema.Compile(definition.Entry.ConfigSchema)
		if err != nil {
			return nil, fmt.Errorf("compile HTTP schema: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := schema.Validate(raw, ""); err != nil {
			validationErr = err
			continue
		}
		if err := catalog.CheckCustomHTTPConfig(raw); err != nil {
			return nil, err
		}
		var config struct {
			Services []struct {
				Port int `json:"port"`
			} `json:"services"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("decode HTTP services: %w", err)
		}
		ports := make([]int, 0, len(config.Services))
		for _, service := range config.Services {
			ports = append(ports, service.Port)
		}
		return ports, nil
	}
	if validationErr != nil {
		return nil, fmt.Errorf("validate HTTP config: %w", validationErr)
	}

	return nil, fmt.Errorf("HTTP descriptor missing")
}
