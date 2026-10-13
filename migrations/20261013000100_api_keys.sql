-- +goose Up
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_action_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_action_check CHECK (action IN (
    'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
    'session.created', 'session.revoked', 'api_key.created', 'api_key.revoked'
));
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_resource_kind_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_resource_kind_check
    CHECK (resource_kind IN ('organization', 'session', 'api_key'));

CREATE TABLE operator_api_keys (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    prefix TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    rate_window TIMESTAMPTZ NOT NULL DEFAULT now(),
    rate_count BIGINT NOT NULL DEFAULT 0 CHECK (rate_count >= 0)
);

CREATE INDEX operator_api_keys_user_id_idx ON operator_api_keys(user_id);

-- +goose Down
DROP TABLE operator_api_keys;

-- Preserve key audit history on rollback while rejecting new key entries.
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_action_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_action_check CHECK (action IN (
    'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
    'session.created', 'session.revoked'
)) NOT VALID;
ALTER TABLE auth_audit DROP CONSTRAINT auth_audit_resource_kind_check;
ALTER TABLE auth_audit ADD CONSTRAINT auth_audit_resource_kind_check
    CHECK (resource_kind IN ('organization', 'session')) NOT VALID;
