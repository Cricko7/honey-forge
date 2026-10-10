package catalog

import (
	"encoding/json"

	"honey-forge/internal/contract"
)

// StructuredEventSchemas returns fresh descriptors for service runtimes that
// implement authentication or actions. They are deliberately not installed on TCP.
func StructuredEventSchemas() []EventDescriptor {
	return []EventDescriptor{
		{EventType: "service.auth_attempt", Title: "Authentication attempt", DataSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["service","username","password","outcome","truncated"],"properties":{"service":{"type":"string","minLength":1,"maxLength":64},"username":{"type":["string","null"],"maxLength":1024},"password":{"type":["string","null"],"maxLength":1024},"outcome":{"type":"string","enum":["accepted","rejected","unknown"]},"truncated":{"type":"boolean"}}}`)},
		{EventType: "service.action", Title: "Service action", DataSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"required":["service","action_kind","input","outcome","truncated"],"properties":{"service":{"type":"string","minLength":1,"maxLength":64},"action_kind":{"type":"string","enum":["command","request"]},"input":{"type":"string","maxLength":4096},"outcome":{"type":"string","enum":["accepted","rejected","unknown"]},"truncated":{"type":"boolean"}}}`)},
	}
}

func checkStructuredBytes(event string, raw json.RawMessage) error {
	var data struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
		Input    string  `json:"input"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return contract.NewError("telemetry_invalid")
	}
	if event == "service.auth_attempt" && (data.Username != nil && len(*data.Username) > 1024 || data.Password != nil && len(*data.Password) > 1024) || event == "service.action" && len(data.Input) > 4096 {
		return contract.NewError("telemetry_invalid")
	}
	return nil
}
