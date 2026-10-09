package auth

import authtypes "github.com/Cricko7/honey-forge/src/backend/modules/auth/types"

const (
	RoleAdmin  = authtypes.RoleAdmin
	RoleViewer = authtypes.RoleViewer
	SessionTTL = authtypes.SessionTTL
)

type User = authtypes.User
type Organization = authtypes.Organization
type SessionView = authtypes.SessionView
type AuthContext = authtypes.AuthContext
type ResolvedSession = authtypes.ResolvedSession
type JoinCode = authtypes.JoinCode
type OrganizationInput = authtypes.OrganizationInput
type RegisterRequest = authtypes.RegisterRequest
type LoginRequest = authtypes.LoginRequest
type RotateJoinCodeRequest = authtypes.RotateJoinCodeRequest
type Session = authtypes.Session
