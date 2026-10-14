package catalog

import (
	_ "embed"
	"encoding/json"
)

//go:embed schemas/tcp-banner-1.json
var tcpBannerJSON []byte

//go:embed schemas/custom-tcp-1.json
var customTCPJSON []byte

//go:embed schemas/custom-http-1.json
var customHTTPJSON []byte

//go:embed schemas/custom-http-2.json
var customHTTPPipelineJSON []byte

//go:embed schemas/redis-emulator-1.json
var redisEmulatorJSON []byte

//go:embed schemas/honeytoken-http-1.json
var honeytokenHTTPJSON []byte

//go:embed schemas/ssh-high-interaction-1.json
var sshHighInteractionJSON []byte

//go:embed schemas/ssh-high-interaction-2.json
var sshHostTelemetryJSON []byte

// BuiltinDefinitions returns fresh copies of the installed trap descriptors.
func BuiltinDefinitions() []Definition {
	var definitions []Definition
	for _, raw := range [][]byte{tcpBannerJSON, redisEmulatorJSON, honeytokenHTTPJSON, sshHighInteractionJSON, sshHostTelemetryJSON, customTCPJSON, customHTTPJSON, customHTTPPipelineJSON} {
		var entry CatalogEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			panic("invalid embedded trap descriptor: " + err.Error())
		}

		definitions = append(definitions, Definition{
			Entry:                  entry,
			SupportsAuthentication: entry.TypeID == "redis-emulator",
			SupportsServiceActions: entry.TypeID == "redis-emulator",
		})
	}

	return definitions
}
