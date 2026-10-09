package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"time"
)

func newUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}

	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:])
}

func newOpaque(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}

	return base64.RawURLEncoding.EncodeToString(value)
}

func validOpaque(value string, size int) bool {
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

func hashToken(value string) []byte {
	hash := sha256.Sum256([]byte(value))

	return hash[:]
}

func csrfToken(raw string) string {
	hash := sha256.Sum256([]byte("csrf:" + raw))

	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func newSession(userID string, now time.Time) (session, string, string) {
	raw := newOpaque(32)
	csrf := csrfToken(raw)

	return session{
		ID:        newUUID(),
		UserID:    userID,
		Hash:      hashToken(raw),
		CSRFHash:  hashToken(csrf),
		ExpiresAt: now.Add(sessionTTL),
	}, raw, csrf
}

func CheckCSRF(sess ResolvedSession, token string) error {
	if !validOpaque(token, 43) || subtle.ConstantTimeCompare(sess.csrfHash, hashToken(token)) != 1 {
		return ErrCSRF
	}

	return nil
}
