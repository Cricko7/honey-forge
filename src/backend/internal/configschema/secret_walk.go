package configschema

import (
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func clearedPath(path string, clears map[string]bool) bool {
	for clear := range clears {
		if path == clear || strings.HasPrefix(path, clear+"/") {
			return true
		}
	}
	return false
}

// Cycle detection is scoped to one JSON value. Re-entering a recursive schema
// at a concrete child is valid and must not hide its writeOnly annotations.
func descendSecret(schema *jsonschema.Schema, value any, path string, set map[string]bool, seen map[*jsonschema.Schema]bool) {
	if value != nil {
		seen = map[*jsonschema.Schema]bool{}
	}
	walkSecrets(schema, value, path, set, seen)
}
