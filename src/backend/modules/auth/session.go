package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"time"
)

func NewUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}

	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:])
}

func NewOpaque(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}

	return base64.RawURLEncoding.EncodeToString(value)
}

func ValidOpaque(value string, size int) bool {
	if len(value) != size {
		return false
	}

	for _, char := range value {
		if !(char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}

	return true
}

func HashToken(value string) []byte {
	hash := sha256.Sum256([]byte(value))

	return hash[:]
}

func CSRFToken(raw string) string {
	hash := sha256.Sum256([]byte("csrf:" + raw))

	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func NewSession(userID string, now time.Time) (Session, string, string) {
	raw := NewOpaque(32)
	csrf := CSRFToken(raw)

	return Session{
		ID:        NewUUID(),
		UserID:    userID,
		Hash:      HashToken(raw),
		CSRFHash:  HashToken(csrf),
		ExpiresAt: now.Add(SessionTTL),
	}, raw, csrf
}

func CheckCSRF(sess ResolvedSession, token string) error {
	if !ValidOpaque(token, 43) || subtle.ConstantTimeCompare(sess.CSRFHash, HashToken(token)) != 1 {
		return ErrCSRF
	}

	return nil
}
