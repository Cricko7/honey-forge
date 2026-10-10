// Package honeytrap serves decoy artifacts on the trap, away from the control API.
package honeytrap

import (
	"context"
	"encoding/json"
	"fmt"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

type Token struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

type Config struct {
	Services []struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	} `json:"services"`
	Tokens []Token `json:"tokens"`
}

func ParseConfig(ctx context.Context, snapshot profiles.Snapshot) (Config, error) {
	var config Config
	if snapshot.TypeID != "honeytoken-http" || snapshot.TypeVersion != 1 || snapshot.ProfileRevision < 1 {
		return config, fmt.Errorf("unsupported trap snapshot")
	}

	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return config, fmt.Errorf("encode configuration: %w", err)
	}

	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		return config, err
	}
	service, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		return config, fmt.Errorf("load catalog: %w", err)
	}
	if err := service.CheckConfig(ctx, snapshot.TypeID, 1, raw); err != nil {
		return config, fmt.Errorf("validate configuration: %w", err)
	}

	if err := json.Unmarshal(raw, &config); err != nil {
		return config, fmt.Errorf("decode configuration: %w", err)
	}

	return config, nil
}
