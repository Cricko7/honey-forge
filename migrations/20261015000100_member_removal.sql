-- +goose Up
ALTER TABLE users ADD COLUMN removed_at timestamptz;
ALTER TABLE users ADD CONSTRAINT users_removed_access_check
    CHECK (removed_at IS NULL OR (role = 'viewer' AND NOT trap_management));
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_action_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_action_check CHECK (action IN (
    'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
    'session.created', 'session.revoked', 'api_key.created', 'api_key.revoked',
    'member.trap_management_granted', 'member.trap_management_revoked',
    'session.admin_confirmed', 'session.admin_locked', 'member.removed'
));

-- +goose Down
-- Refuse rollback if it would reactivate removed accounts.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM users WHERE removed_at IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back member removal while removed accounts exist';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_action_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_action_check CHECK (action IN (
    'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
    'session.created', 'session.revoked', 'api_key.created', 'api_key.revoked',
    'member.trap_management_granted', 'member.trap_management_revoked',
    'session.admin_confirmed', 'session.admin_locked'
)) NOT VALID;
ALTER TABLE users DROP CONSTRAINT users_removed_access_check;
ALTER TABLE users DROP COLUMN removed_at;
