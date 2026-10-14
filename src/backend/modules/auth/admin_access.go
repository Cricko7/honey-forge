package auth

import "time"

const AdminAccessTTL = 15 * time.Minute

type AdminAccessView struct {
	ExpiresAt *time.Time `json:"expires_at"`
}

type AdminPasswordRequest struct {
	Password string `json:"password" binding:"required,min=12,max=128,maxbytes=512" validate:"required,min=12,max=128,maxbytes=512"`
}

type AdminPasswordRateError struct{ RetryAfter int }

func (e *AdminPasswordRateError) Error() string {
	return "admin password confirmation rate limit exceeded"
}
