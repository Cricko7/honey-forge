package types

import "time"

const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
	SessionTTL = 7 * 24 * time.Hour
)

type User struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Email          string    `json:"email"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
}

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionView struct {
	User         User         `json:"user"`
	Organization Organization `json:"organization"`
	ExpiresAt    time.Time    `json:"expires_at"`
	CSRFToken    string       `json:"csrf_token"`
}

// AuthContext contains only identity verified by the server.
type AuthContext struct {
	UserID         string
	OrganizationID string
	Role           string
}

type ResolvedSession struct {
	View     SessionView
	ID       string
	CSRFHash []byte
}

func (s ResolvedSession) AuthContext() AuthContext {
	return AuthContext{UserID: s.View.User.ID, OrganizationID: s.View.User.OrganizationID, Role: s.View.User.Role}
}

type JoinCode struct {
	Code      string    `json:"code"`
	Revision  int32     `json:"revision"`
	RotatedAt time.Time `json:"rotated_at"`
}

type OrganizationInput struct {
	Mode     string `json:"mode" binding:"required,oneof=create join" validate:"required,oneof=create join"`
	Name     string `json:"name,omitempty" binding:"required_if=Mode create,excluded_if=Mode join,omitempty,orgname" validate:"required_if=Mode create,excluded_if=Mode join,omitempty,orgname"`
	JoinCode string `json:"join_code,omitempty" binding:"required_if=Mode join,excluded_if=Mode create,omitempty,joincode" validate:"required_if=Mode join,excluded_if=Mode create,omitempty,joincode"`
}

type RegisterRequest struct {
	Email        string             `json:"email" binding:"required,operatoremail" validate:"required,operatoremail"`
	Password     string             `json:"password" binding:"required,min=12,max=128,maxbytes=512" validate:"required,min=12,max=128,maxbytes=512"`
	Organization *OrganizationInput `json:"organization" binding:"required" validate:"required"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,operatoremail" validate:"required,operatoremail"`
	Password string `json:"password" binding:"required,min=12,max=128,maxbytes=512" validate:"required,min=12,max=128,maxbytes=512"`
}

type RotateJoinCodeRequest struct {
	ExpectedRevision int64 `json:"expected_revision" binding:"required,min=1,max=2147483647" validate:"required,min=1,max=2147483647"`
}

type Session struct {
	ID        string
	UserID    string
	Hash      []byte
	CSRFHash  []byte
	ExpiresAt time.Time
}
