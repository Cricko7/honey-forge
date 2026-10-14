package events

import (
	"encoding/json"

	"honey-forge/internal/contract"
	"honey-forge/modules/profiles"
)

func checkHTTPBlockSnapshot(event AgentEvent, snapshot profiles.Snapshot) error {
	if event.EventType != "http.block" {
		return nil
	}

	invalid := contract.NewError("telemetry_invalid")
	var data struct {
		ID   string `json:"node_id"`
		Kind string `json:"kind"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return invalid
	}

	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return invalid
	}

	var config struct {
		Blocks []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"blocks"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return invalid
	}

	for _, block := range config.Blocks {
		if block.ID == data.ID && block.Kind == data.Kind {
			return nil
		}
	}

	return invalid
}
