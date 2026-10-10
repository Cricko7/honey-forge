package catalog

import (
	"encoding/json"
	"path"
	"strings"

	"honey-forge/internal/contract"
)

func checkHoneytokenConfig(raw json.RawMessage) error {
	var config struct {
		Tokens []struct {
			ID    string `json:"id"`
			Path  string `json:"path"`
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return contract.NewError("config_invalid")
	}
	ids, paths, keys := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, token := range config.Tokens {
		if ids[token.ID] || paths[token.Path] || path.Clean(token.Path) != token.Path || strings.HasPrefix(token.Path, "/artifacts/") || token.Path == "/artifacts" || len(token.Value) > 4096 {
			return contract.NewError("config_invalid")
		}
		if token.Kind == "key" {
			if len(token.Value) < 16 || len(token.Value) > 128 || strings.ContainsAny(token.Value, " \t\r\n") || keys[token.Value] {
				return contract.NewError("config_invalid")
			}
			keys[token.Value] = true
		}
		ids[token.ID], paths[token.Path] = true, true
	}
	return nil
}
