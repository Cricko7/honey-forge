package auth

import "time"

const PresenceWindow = 90 * time.Second

type Member struct {
	User
	PermissionRevision int32      `json:"permission_revision"`
	Online             bool       `json:"online"`
	LastSeenAt         *time.Time `json:"last_seen_at"`
}

type MemberPage struct {
	Creator    *User    `json:"creator"`
	Items      []Member `json:"items"`
	NextCursor *string  `json:"next_cursor"`
}

type TrapPermissionRequest struct {
	CanManageTraps   *bool `json:"can_manage_traps" binding:"required" validate:"required"`
	ExpectedRevision int32 `json:"expected_revision" binding:"required,min=1" validate:"required,min=1"`
}
