package mutation

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"honey-forge/internal/contract"
)

// Fingerprint receives values already normalized by the feature, not an unbound
// request body. Re-encoding object maps makes JSON key order irrelevant.
func Fingerprint(normalized json.RawMessage) ([32]byte, error) {
	if err := contract.CheckJSON(normalized); err != nil {
		return [32]byte{}, contract.NewError("invalid_json")
	}
	d := json.NewDecoder(bytes.NewReader(normalized))
	d.UseNumber()
	var v map[string]any
	if err := d.Decode(&v); err != nil || v == nil {
		return [32]byte{}, contract.NewError("invalid_json")
	}
	b, err := json.Marshal(normalizeNumbers(v))
	if err != nil {
		return [32]byte{}, contract.NewError("invalid_json")
	}
	return sha256.Sum256(b), nil
}
func equalFingerprint(a, b []byte) bool { return subtle.ConstantTimeCompare(a, b) == 1 }
