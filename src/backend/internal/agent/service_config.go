package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"honey-forge/internal/configschema"
	"honey-forge/modules/catalog"
	"honey-forge/modules/commands"
	"honey-forge/modules/profiles"
	"time"
)

func (s *Service) FlushInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	configuration := s.journal.LoadState().Configuration
	if configuration == nil {
		return 100 * time.Millisecond
	}
	flush, err := validateSnapshot(context.Background(), *configuration)
	if err != nil {
		return 100 * time.Millisecond
	}
	return time.Duration(flush) * time.Millisecond
}

func (s *Service) HeartbeatInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.journal.LoadState().Configuration
	if snapshot == nil {
		return 10 * time.Second
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return 10 * time.Second
	}
	var config struct {
		Management struct {
			Heartbeat int `json:"heartbeat_interval_seconds"`
		} `json:"management"`
	}
	if json.Unmarshal(raw, &config) != nil || config.Management.Heartbeat < 5 || config.Management.Heartbeat > 10 {
		return 10 * time.Second
	}
	return time.Duration(config.Management.Heartbeat) * time.Second
}

func validateSnapshot(ctx context.Context, snapshot profiles.Snapshot) (int, error) {
	if snapshot.ProfileRevision < 1 {
		return 0, fmt.Errorf("unsupported trap snapshot")
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return 0, fmt.Errorf("encode configuration: %w", err)
	}
	for _, definition := range catalog.BuiltinDefinitions() {
		if string(definition.Entry.TypeID) != snapshot.TypeID || int32(definition.Entry.TypeVersion) != snapshot.TypeVersion {
			continue
		}
		schema, err := configschema.Compile(definition.Entry.ConfigSchema)
		if err != nil {
			return 0, fmt.Errorf("compile configuration: %w", err)
		}
		if err := schema.Validate(raw, ""); err != nil {
			return 0, fmt.Errorf("validate configuration: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		var config struct {
			Management struct {
				Flush int `json:"telemetry_flush_interval_ms"`
			} `json:"management"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			return 0, fmt.Errorf("decode configuration: %w", err)
		}
		return config.Management.Flush, nil
	}
	return 0, fmt.Errorf("unsupported trap type")
}

func runtimeError(code string) *commands.RuntimeError {
	e := &commands.RuntimeError{Code: code}
	if err := commands.NormalizeRuntimeError(e); err != nil {
		return &commands.RuntimeError{Code: "runtime_start_failed", Message: "Runtime could not start"}
	}
	return e
}
