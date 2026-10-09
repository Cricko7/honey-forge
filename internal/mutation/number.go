package mutation

import (
	"encoding/json"
	"math/big"
	"strings"
)

// Normalize decimal spelling exactly, without float64 rounding or expanding
// scientific notation into a potentially enormous string.
func canonicalNumber(number json.Number) json.Number {
	text := string(number)
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	mantissa, exponentText, hasExponent := strings.Cut(strings.ToLower(text), "e")
	exponent := new(big.Int)
	if hasExponent {
		if _, ok := exponent.SetString(exponentText, 10); !ok {
			return number
		}
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		return json.Number("0")
	}
	trimmed := strings.TrimRight(digits, "0")
	adjustment := int64(len(digits) - len(trimmed) - len(fraction))
	exponent.Add(exponent, big.NewInt(adjustment))
	if negative {
		trimmed = "-" + trimmed
	}
	return json.Number(trimmed + "e" + exponent.String())
}
func normalizeNumbers(v any) any {
	switch value := v.(type) {
	case json.Number:
		return canonicalNumber(value)
	case map[string]any:
		for key, child := range value {
			value[key] = normalizeNumbers(child)
		}
	case []any:
		for i, child := range value {
			value[i] = normalizeNumbers(child)
		}
	}
	return v
}
