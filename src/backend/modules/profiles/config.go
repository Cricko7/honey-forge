package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Cricko7/honey-forge/src/backend/internal/platform/httpx"
)

func Canonical(v any) ([]byte, error) { return httpx.CanonicalJSON(v) }

func EqualJSON(a, b any) bool {
	x, e := Canonical(a)
	if e != nil {
		return false
	}

	y, e := Canonical(b)
	return e == nil && bytes.Equal(x, y)
}

func CloneObject(c Object) Object {
	if c == nil {
		return nil
	}

	result := make(Object, len(c))
	for k, v := range c {
		result[k] = cloneValue(v)
	}

	return result
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return CloneObject(x)
	case []any:
		r := make([]any, len(x))
		for i, v := range x {
			r[i] = cloneValue(v)
		}

		return r
	default:
		return x
	}
}

func CheckConfig(ctx context.Context, typ Type, c Object) error {
	if c == nil {
		return ErrConfigInvalid
	}

	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if len(raw) > 128<<10 {
		return ErrConfigTooLarge
	}

	if typ.CheckConfig == nil {
		return ErrUnavailable
	}

	return typ.CheckConfig(ctx, CloneObject(c))
}

func InstalledSecrets(typ Type, c Object) []string {
	paths := []string{}
	if typ.SecretPaths == nil {
		return paths
	}

	seen := map[string]bool{}
	for _, path := range typ.SecretPaths(c) {
		if _, exists := PointerGet(c, path); exists && !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}

	sort.Strings(paths)
	return paths
}

func Redacted(p Profile) Profile {
	p.Config = CloneObject(p.Config)
	p.SecretFieldsSet = append([]string{}, p.SecretFieldsSet...)
	for _, path := range p.SecretFieldsSet {
		pointerRemove(p.Config, path)
	}

	return p
}

func PatchConfig(typ Type, p Profile, req PatchRequest) (Object, error) {
	clear := map[string]bool{}
	for _, path := range req.ClearSecretFields {
		if typ.IsSecretPath == nil || !typ.IsSecretPath(path) {
			return nil, &ValidationError{
				ErrValidation,
				[]httpx.FieldError{{
					Path:    "/clear_secret_fields",
					Code:    "invalid_secret_path",
					Message: "Expected a writeOnly field path",
				}},
			}
		}

		if _, exists := PointerGet(req.Config, path); exists {
			return nil, ErrValidation
		}

		clear[path] = true
	}

	c := CloneObject(p.Config)
	if req.Config != nil {
		c = CloneObject(req.Config)
		for _, path := range p.SecretFieldsSet {
			if clear[path] {
				continue
			}

			if _, exists := PointerGet(c, path); exists {
				continue
			}

			value, exists := PointerGet(p.Config, path)
			if exists {
				if !pointerPut(c, p.Config, path, cloneValue(value)) {
					return nil, ErrConfigInvalid
				}
			}
		}
	}

	for path := range clear {
		pointerRemove(c, path)
	}

	return c, nil
}
