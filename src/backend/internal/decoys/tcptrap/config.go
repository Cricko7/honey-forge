// Package tcptrap implements the catalog's low-interaction tcp-banner/1 trap.
package tcptrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"honey-forge/internal/configschema"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
)

type Listener struct {
	Name             string `json:"name"`
	Port             int    `json:"port"`
	Banner           string `json:"banner"`
	CloseAfterBanner bool   `json:"close_after_banner"`
}

type Config struct {
	Listeners []Listener `json:"listeners"`
	Logging   struct {
		Capture  bool `json:"capture_payload"`
		MaxBytes int  `json:"max_payload_bytes"`
	} `json:"logging"`
	Management struct {
		Heartbeat int `json:"heartbeat_interval_seconds"`
		Flush     int `json:"telemetry_flush_interval_ms"`
	} `json:"management"`
}

func ParseConfig(ctx context.Context, snapshot profiles.Snapshot) (Config, error) {
	var config Config
	if snapshot.TypeID != "tcp-banner" || snapshot.TypeVersion != 1 || snapshot.ProfileRevision < 1 {
		return config, fmt.Errorf("unsupported trap snapshot")
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return config, fmt.Errorf("encode configuration: %w", err)
	}
	schema, err := configschema.Compile(catalog.BuiltinDefinitions()[0].Entry.ConfigSchema)
	if err != nil {
		return config, fmt.Errorf("compile TCP schema: %w", err)
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
	names, ports := map[string]bool{}, map[int]bool{}
	for _, l := range config.Listeners {
		if names[l.Name] || ports[l.Port] || len(l.Banner) > 4096 {
			return config, fmt.Errorf("duplicate listener or oversized banner")
		}
		names[l.Name] = true
		ports[l.Port] = true
	}
	if !config.Logging.Capture && config.Logging.MaxBytes != 0 || config.Logging.Capture && config.Logging.MaxBytes < 1 {
		return config, fmt.Errorf("inconsistent payload capture settings")
	}
	return config, nil
}

func fmtPort(port int) string { return strconv.Itoa(port) }
