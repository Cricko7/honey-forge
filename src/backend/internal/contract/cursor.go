package contract

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type CursorScope struct {
	OrganizationID ID
	Collection     string
	Filters        string
}

// Filters must be the canonical normalized filters of the feature, not raw query order.
type CursorPosition struct {
	Boundary string
	After    string
}
type CursorCodec struct{ aead cipher.AEAD }

func NewCursorCodec(key []byte) (*CursorCodec, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("cursor key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cursor cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cursor encryption: %w", err)
	}
	return &CursorCodec{aead}, nil
}
func (c *CursorCodec) Encode(scope CursorScope, position CursorPosition) (string, error) {
	if !ValidID(string(scope.OrganizationID)) || scope.Collection == "" || position.Boundary == "" {
		return "", NewError("invalid_cursor")
	}
	b, err := json.Marshal(position)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("cursor nonce: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(c.aead.Seal(nonce, nonce, b, cursorScope(scope)))
	if len(token) > MaxCursorLength {
		return "", NewError("invalid_cursor")
	}
	return token, nil
}
func (c *CursorCodec) Decode(scope CursorScope, token string) (CursorPosition, error) {
	invalid := NewError("invalid_cursor")
	if token == "" || len(token) > MaxCursorLength {
		return CursorPosition{}, invalid
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(b) < c.aead.NonceSize()+c.aead.Overhead() {
		return CursorPosition{}, invalid
	}
	n := c.aead.NonceSize()
	plain, err := c.aead.Open(nil, b[:n], b[n:], cursorScope(scope))
	if err != nil {
		return CursorPosition{}, invalid
	}
	var p CursorPosition
	if err := json.Unmarshal(plain, &p); err != nil || p.Boundary == "" {
		return CursorPosition{}, invalid
	}
	return p, nil
}
func cursorScope(scope CursorScope) []byte {
	b, err := json.Marshal(struct {
		Organization string
		Collection   string
		Filters      string
	}{strings.ToLower(string(scope.OrganizationID)), scope.Collection, scope.Filters})
	if err != nil {
		panic(err)
	}
	return b
}
