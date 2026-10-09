package catalog

import (
	_ "embed"
	"encoding/json"
)

//go:embed schemas/tcp-banner-1.json
var tcpBannerJSON []byte

// BuiltinDefinitions returns fresh copies of the installed tcp-banner/1 descriptor.
func BuiltinDefinitions() []Definition {
	var entry CatalogEntry
	if err := json.Unmarshal(tcpBannerJSON, &entry); err != nil {
		panic("invalid embedded TCP descriptor: " + err.Error())
	}
	return []Definition{{Entry: entry}}
}
