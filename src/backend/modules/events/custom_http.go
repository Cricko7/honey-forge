package events

import (
	"encoding/json"

	"honey-forge/internal/contract"
	"honey-forge/modules/profiles"
)

func checkHTTPBlockSnapshot(event AgentEvent, snapshot profiles.Snapshot) error {
	if event.EventType != "http.block" && event.EventType != "http.custom" && event.EventType != "http.pipeline_received" {
		return nil
	}

	invalid := contract.NewError("telemetry_invalid")
	var data struct {
		ID       string `json:"node_id"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Severity string `json:"severity"`
		Service  string `json:"service"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return invalid
	}

	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return invalid
	}

	var config struct {
		Services []struct {
			Name  string `json:"name"`
			Entry string `json:"entry"`
		} `json:"services"`
		Blocks []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Config struct {
				Name     string `json:"name"`
				Severity string `json:"severity"`
			} `json:"config"`
		} `json:"blocks"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return invalid
	}

	for _, block := range config.Blocks {
		if event.EventType == "http.pipeline_received" && block.ID == data.ID && block.Kind == "input" {
			for _, service := range config.Services {
				if service.Name == data.Service && service.Entry == data.ID {
					return nil
				}
			}
		}
		if event.EventType == "http.custom" && block.ID == data.ID && block.Kind == "event" && block.Config.Name == data.Name && block.Config.Severity == data.Severity {
			return nil
		}
		if block.ID == data.ID && block.Kind == data.Kind {
			return nil
		}
	}

	return invalid
}
