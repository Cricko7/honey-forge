// Package sshtrap records SSH traffic outside an isolated Linux sandbox.
package sshtrap

import (
	"context"
	"encoding/json"
	"fmt"

	"honey-forge/internal/contract"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

type Config struct {
	Services []struct {
		Name     string `json:"name"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"services"`
	Session struct {
		MaxSeconds        int   `json:"max_seconds"`
		MaxConnections    int   `json:"max_connections"`
		MaxRecordingBytes int64 `json:"max_recording_bytes"`
		MaxUploadBytes    int64 `json:"max_upload_bytes"`
	} `json:"session"`
}

func ParseConfig(ctx context.Context, snapshot profiles.Snapshot) (Config, error) {
	var config Config
	if snapshot.TypeID != "ssh-high-interaction" || snapshot.TypeVersion != 1 || snapshot.ProfileRevision < 1 {
		return config, fmt.Errorf("unsupported SSH snapshot")
	}

	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return config, fmt.Errorf("encode SSH config: %w", err)
	}

	codec, err := contract.NewCursorCodec(make([]byte, 32))
	if err != nil {
		return config, err
	}

	service, err := catalog.NewService(catalog.BuiltinDefinitions(), codec)
	if err != nil {
		return config, err
	}

	if err := service.CheckConfig(ctx, snapshot.TypeID, 1, raw); err != nil {
		return config, fmt.Errorf("validate SSH config: %w", err)
	}

	if err := json.Unmarshal(raw, &config); err != nil {
		return config, fmt.Errorf("decode SSH config: %w", err)
	}

	return config, nil
}
