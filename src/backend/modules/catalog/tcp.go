package catalog

import (
	"context"
	"encoding/json"
	"honey-forge/internal/contract"
	"math/big"
)

type tcpConfig struct {
	Listeners []struct {
		Name   string        `json:"name"`
		Port   schemaInteger `json:"port"`
		Banner string        `json:"banner"`
	} `json:"listeners"`
	Logging struct {
		Capture  bool          `json:"capture_payload"`
		MaxBytes schemaInteger `json:"max_payload_bytes"`
	} `json:"logging"`
}
type tcpPayload struct {
	Listener  string        `json:"listener_name"`
	Base64    string        `json:"payload_base64"`
	Captured  schemaInteger `json:"captured_bytes"`
	Original  json.Number   `json:"original_bytes"`
	Truncated bool          `json:"truncated"`
}

func checkTCPConfig(raw json.RawMessage) error {
	var config tcpConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return contract.NewError("config_invalid")
	}
	names, ports := map[string]bool{}, map[schemaInteger]bool{}
	for _, listener := range config.Listeners {
		if names[listener.Name] || ports[listener.Port] || len(listener.Banner) > 4096 {
			return contract.NewError("config_invalid")
		}
		names[listener.Name] = true
		ports[listener.Port] = true
	}
	if !config.Logging.Capture && config.Logging.MaxBytes != 0 || config.Logging.Capture && config.Logging.MaxBytes < 1 {
		return contract.NewError("config_invalid")
	}
	return nil
}

func checkTCPPayload(raw json.RawMessage) error {
	var payload tcpPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return contract.NewError("telemetry_invalid")
	}
	decoded, err := contract.DecodeBytes(payload.Base64, 4096)
	if err != nil || schemaInteger(len(decoded)) != payload.Captured {
		return contract.NewError("telemetry_invalid")
	}
	// Schema accepts arbitrary JSON integers; compare without losing precision or overflowing int64.
	original, ok := new(big.Rat).SetString(string(payload.Original))
	if !ok || !original.IsInt() || original.Sign() <= 0 {
		return contract.NewError("telemetry_invalid")
	}
	captured := new(big.Rat).SetInt64(int64(payload.Captured))
	comparison := captured.Cmp(original)
	if comparison > 0 || payload.Truncated != (comparison < 0) {
		return contract.NewError("telemetry_invalid")
	}
	return nil
}

// CheckPayloadCapture checks one fragment against a pinned configuration and the
// captured count for that connection. The ingestion caller persists the returned
// count atomically with its batch, and retains it across batches/retries.
func (s *Service) CheckPayloadCapture(ctx context.Context, id string, version contract.TypeVersion, config, data json.RawMessage, capturedSoFar int64) (int64, error) {
	if err := s.CheckConfig(ctx, id, version, config); err != nil {
		return capturedSoFar, err
	}
	if err := s.CheckEvent(ctx, id, version, "tcp.payload_received", data); err != nil {
		return capturedSoFar, err
	}
	if id != "tcp-banner" || version != 1 {
		return capturedSoFar, contract.NewError("telemetry_invalid")
	}
	var settings tcpConfig
	var payload tcpPayload
	if err := json.Unmarshal(config, &settings); err != nil {
		return capturedSoFar, contract.NewError("config_invalid")
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return capturedSoFar, contract.NewError("telemetry_invalid")
	}
	limit := int64(settings.Logging.MaxBytes)
	if !settings.Logging.Capture || capturedSoFar < 0 || capturedSoFar > limit || int64(payload.Captured) > limit-capturedSoFar {
		return capturedSoFar, contract.NewError("telemetry_invalid")
	}
	found := false
	for _, listener := range settings.Listeners {
		if listener.Name == payload.Listener {
			found = true
			break
		}
	}
	if !found {
		return capturedSoFar, contract.NewError("telemetry_invalid")
	}
	return capturedSoFar + int64(payload.Captured), nil
}
