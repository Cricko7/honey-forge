package httpx

import "honey-forge/internal/platform/jsonvalue"

func CanonicalJSON(v any) ([]byte, error) { return jsonvalue.Canonical(v) }
