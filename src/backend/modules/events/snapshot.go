package events

import (
	"encoding/json"

	"honey-forge/internal/contract"
	"honey-forge/modules/profiles"
)

func checkServiceSnapshot(event AgentEvent, snapshot profiles.Snapshot) error {
	invalid := contract.NewError("telemetry_invalid")
	var data struct {
		Service string `json:"service"`
	}
	if json.Unmarshal(event.Data, &data) != nil || data.Service == "" {
		return invalid
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil {
		return invalid
	}
	var config struct {
		Services []struct {
			Name string `json:"name"`
			Port int    `json:"port"`
		} `json:"services"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return invalid
	}
	for _, service := range config.Services {
		if service.Name == data.Service && (service.Port == 0 || event.Destination.Protocol == "tcp" && event.Destination.Port == service.Port) {
			return nil
		}
	}
	return invalid
}
