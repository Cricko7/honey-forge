-- +goose Up
ALTER TABLE operator_sessions ADD COLUMN admin_verified_until timestamptz;
ALTER TABLE operator_sessions ADD CONSTRAINT operator_admin_expiry_check
    CHECK (admin_verified_until IS NULL OR admin_verified_until <= expires_at);
ALTER TABLE users ADD COLUMN admin_auth_window timestamptz NOT NULL DEFAULT now();
ALTER TABLE users ADD COLUMN admin_auth_attempts integer NOT NULL DEFAULT 0 CHECK (admin_auth_attempts >= 0);
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_action_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_action_check CHECK (action IN (
    'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
    'session.created', 'session.revoked', 'api_key.created', 'api_key.revoked',
    'member.trap_management_granted', 'member.trap_management_revoked',
    'session.admin_confirmed', 'session.admin_locked'
));

-- +goose Down
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_action_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_action_check CHECK (action IN (
    'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
    'session.created', 'session.revoked', 'api_key.created', 'api_key.revoked',
    'member.trap_management_granted', 'member.trap_management_revoked'
)) NOT VALID;
ALTER TABLE users DROP COLUMN admin_auth_window, DROP COLUMN admin_auth_attempts;
ALTER TABLE operator_sessions DROP CONSTRAINT operator_admin_expiry_check;
ALTER TABLE operator_sessions DROP COLUMN admin_verified_until;
