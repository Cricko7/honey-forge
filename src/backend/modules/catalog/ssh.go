package catalog

import (
	"encoding/base64"
	"encoding/json"

	"honey-forge/internal/contract"
)

func checkSSHBytes(raw json.RawMessage) error {
	var data struct {
		Payload string `json:"payload_base64"`
		Bytes   int    `json:"bytes"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return contract.NewError("telemetry_invalid")
	}

	decoded, err := base64.StdEncoding.Strict().DecodeString(data.Payload)
	if err != nil || len(decoded) != data.Bytes || base64.StdEncoding.EncodeToString(decoded) != data.Payload {
		return contract.NewError("telemetry_invalid")
	}

	return nil
}
