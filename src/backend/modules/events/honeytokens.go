package events

import (
	"encoding/json"

	"honey-forge/internal/contract"
	"honey-forge/modules/profiles"
)

func checkHoneytokenSnapshot(event AgentEvent, snapshot profiles.Snapshot) error {
	if err := checkServiceSnapshot(event, snapshot); err != nil {
		return err
	}
	var data struct {
		ID   string `json:"token_id"`
		Kind string `json:"kind"`
	}
	raw, err := json.Marshal(snapshot.Config)
	if err != nil || json.Unmarshal(event.Data, &data) != nil {
		return contract.NewError("telemetry_invalid")
	}
	var config struct {
		Tokens []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return contract.NewError("telemetry_invalid")
	}
	for _, token := range config.Tokens {
		if token.ID == data.ID && token.Kind == data.Kind {
			return nil
		}
	}
	return contract.NewError("telemetry_invalid")
}
