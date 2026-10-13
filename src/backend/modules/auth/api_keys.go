package auth

import (
	"net/http"
	"strings"
	"time"
)

type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"created_at"`
	UserID    string    `json:"-"`
	Hash      []byte    `json:"-"`
}

type CreateAPIKeyRequest struct {
	Name string `json:"name" binding:"required,min=1,max=120" validate:"required,min=1,max=120"`
}

type APIKeyRateError struct {
	RetryAfter int
}

func (e *APIKeyRateError) Error() string {
	return "API key rate limit exceeded"
}

// BearerToken rejects ambiguous headers; callers never fall back to cookies.
func BearerToken(header http.Header) (string, error) {
	values := header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer hf_") {
		return "", ErrUnauthorized
	}

	raw := strings.TrimPrefix(values[0], "Bearer ")
	if !ValidOpaque(strings.TrimPrefix(raw, "hf_"), 43) {
		return "", ErrUnauthorized
	}

	return raw, nil
}
