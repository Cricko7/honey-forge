-- +goose Up
ALTER TABLE users ADD COLUMN trap_management boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN permission_revision integer NOT NULL DEFAULT 1 CHECK (permission_revision >= 1);
ALTER TABLE users ADD CONSTRAINT users_id_organization_key UNIQUE (id, organization_id);
ALTER TABLE organizations ADD COLUMN creator_user_id uuid;

-- Preserve identity independently of audit retention. Ambiguous historical
-- organizations remain without a creator; never assign ownership speculatively.
UPDATE organizations o SET creator_user_id = (
    SELECT a.actor_user_id FROM auth_audit a
    JOIN users u ON u.id = a.actor_user_id AND u.organization_id = o.id
    WHERE a.organization_id = o.id AND a.action = 'organization.created'
    ORDER BY a.occurred_at, a.id LIMIT 1
);
UPDATE organizations o SET creator_user_id = (
    SELECT min(u.id::text)::uuid FROM users u WHERE u.organization_id = o.id AND u.role = 'admin'
    HAVING count(*) = 1
) WHERE creator_user_id IS NULL;
ALTER TABLE organizations ADD CONSTRAINT organizations_creator_member_fk
    FOREIGN KEY (creator_user_id, id) REFERENCES users (id, organization_id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX users_organization_id_idx ON users (organization_id, id);
ALTER TABLE operator_sessions ADD COLUMN last_seen_at timestamptz;

ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_action_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_action_check CHECK (action IN (
    'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
    'session.created', 'session.revoked', 'api_key.created', 'api_key.revoked',
    'member.trap_management_granted', 'member.trap_management_revoked'
));
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_resource_kind_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_resource_kind_check
    CHECK (resource_kind IN ('organization', 'session', 'api_key', 'user'));

-- +goose Down
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_action_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_action_check CHECK (action IN (
    'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
    'session.created', 'session.revoked', 'api_key.created', 'api_key.revoked'
)) NOT VALID;
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_resource_kind_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_resource_kind_check
    CHECK (resource_kind IN ('organization', 'session', 'api_key')) NOT VALID;
ALTER TABLE operator_sessions DROP COLUMN last_seen_at;
DROP INDEX users_organization_id_idx;
ALTER TABLE organizations DROP CONSTRAINT organizations_creator_member_fk;
ALTER TABLE organizations DROP COLUMN creator_user_id;
ALTER TABLE users DROP CONSTRAINT users_id_organization_key;
ALTER TABLE users DROP COLUMN trap_management, DROP COLUMN permission_revision;
