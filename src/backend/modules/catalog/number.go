package catalog

import (
	"fmt"
	"math/big"
)

// schemaInteger follows JSON Schema's numeric integer semantics: 1, 1.0 and
// 1e0 represent the same integer. Parsing stays exact rather than using float64.
// Schema validation runs first and applies the domain's field-specific limits.
type schemaInteger int64

func (n *schemaInteger) UnmarshalJSON(raw []byte) error {
	value, ok := new(big.Rat).SetString(string(raw))
	if !ok || !value.IsInt() || !value.Num().IsInt64() {
		return fmt.Errorf("integer outside supported range")
	}
	*n = schemaInteger(value.Num().Int64())
	return nil
}
