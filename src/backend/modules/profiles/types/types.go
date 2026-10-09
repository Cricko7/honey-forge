package types

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Object = map[string]any

type CreateRequest struct {
	RequestID   string `json:"request_id" binding:"required,uuid" validate:"required,uuid"`
	Name        string `json:"name" binding:"required,profilename" validate:"required,profilename"`
	Description string `json:"description" binding:"max=1000" validate:"max=1000"`
	TypeID      string `json:"type_id" binding:"required,profiletype" validate:"required,profiletype"`
	TypeVersion int64  `json:"type_version" binding:"required,min=1,max=2147483647" validate:"required,min=1,max=2147483647"`
	Config      Object `json:"config" binding:"required" validate:"required"`
}

type PatchRequest struct {
	Name              *string  `json:"name" binding:"omitempty,profilename" validate:"omitempty,profilename"`
	Description       *string  `json:"description" binding:"omitempty,max=1000" validate:"omitempty,max=1000"`
	Config            Object   `json:"config"`
	ClearSecretFields []string `json:"clear_secret_fields" binding:"max=100,unique,dive,profilepointer" validate:"max=100,unique,dive,profilepointer"`
}

type Profile struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	TypeID           string    `json:"type_id"`
	TypeVersion      int32     `json:"type_version"`
	InteractionLevel string    `json:"interaction_level"`
	Config           Object    `json:"config"`
	SecretFieldsSet  []string  `json:"secret_fields_set"`
	Revision         int32     `json:"revision"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	OrganizationID   string    `json:"-"`
	Sequence         int64     `json:"-"`
}

// Snapshot is private operator data: it may only be handed to a trusted agent/command.
type Snapshot struct {
	ProfileID       string `json:"profile_id"`
	ProfileRevision int32  `json:"profile_revision"`
	TypeID          string `json:"type_id"`
	TypeVersion     int32  `json:"type_version"`
	Config          Object `json:"config"`
}

type CreateResult struct {
	Profile           Profile
	Replayed, Current bool
}

type Page struct {
	Items      []Profile `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}

type ListOptions struct {
	Limit          int
	Cursor, TypeID string
}

// Type holds a frozen exact-version validator supplied by the catalog adapter.
type Type struct {
	InteractionLevel        string
	AvailableForNewProfiles bool
	CheckConfig             func(context.Context, Object) error
	SecretPaths             func(Object) []string
	IsSecretPath            func(string) bool
}

// Dependencies are functions, so absent neighboring modules can be faked in tests.
type Dependencies struct {
	LookupType      func(context.Context, string, int32) (Type, error)
	HasLiveBindings func(context.Context, string, string, pgx.Tx) (bool, error)
}

type ListQuery struct {
	Limit     int
	TypeID    string
	Boundary  int64
	AfterTime time.Time
	AfterID   string
}
