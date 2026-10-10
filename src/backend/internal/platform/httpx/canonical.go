package httpx

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strings"
)

func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(normalizeNumbers(value))
}

func normalizeNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		mantissa, exponent, _ := strings.Cut(strings.ToLower(string(x)), "e")
		e := new(big.Int)
		if exponent != "" {
			if _, ok := e.SetString(exponent, 10); !ok {
				return x
			}
		}
		sign := ""
		if strings.HasPrefix(mantissa, "-") {
			sign = "-"
			mantissa = mantissa[1:]
		}
		whole, fraction, _ := strings.Cut(mantissa, ".")
		digits := strings.TrimLeft(whole+fraction, "0")
		if digits == "" {
			return json.Number("0")
		}
		trimmed := strings.TrimRight(digits, "0")
		e.Add(e, big.NewInt(int64(len(digits)-len(trimmed)-len(fraction))))
		return json.Number(sign + trimmed + "e" + e.String())
	case map[string]any:
		for k, value := range x {
			x[k] = normalizeNumbers(value)
		}
	case []any:
		for i, value := range x {
			x[i] = normalizeNumbers(value)
		}
	}
	return v
}
