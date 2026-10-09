-- +goose Up
-- Existing users created their organization, so they become admins.
ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'admin'
    CHECK (role IN ('admin', 'viewer'));
ALTER TABLE users ALTER COLUMN role DROP DEFAULT;

-- A fresh independent random code for every existing organization.
ALTER TABLE organizations ADD COLUMN join_code text;
UPDATE organizations SET join_code = translate(
    substring(encode(sha256(gen_random_uuid()::text::bytea), 'base64') FROM 1 FOR 32), '+/', '-_'
);
ALTER TABLE organizations ALTER COLUMN join_code SET NOT NULL;
ALTER TABLE organizations ADD CONSTRAINT organizations_join_code_key UNIQUE (join_code);
ALTER TABLE organizations ADD CONSTRAINT organizations_join_code_format CHECK (join_code ~ '^[A-Za-z0-9_-]{32}$');
ALTER TABLE organizations ADD COLUMN join_code_revision integer NOT NULL DEFAULT 1 CHECK (join_code_revision >= 1);
ALTER TABLE organizations ADD COLUMN join_code_rotated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE organizations DROP CONSTRAINT organizations_name_check;
ALTER TABLE organizations ADD CONSTRAINT organizations_name_check CHECK (char_length(name) BETWEEN 1 AND 100 AND name = btrim(name));

CREATE TABLE operator_sessions (
    id uuid PRIMARY KEY,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    csrf_hash bytea NOT NULL CHECK (octet_length(csrf_hash) = 32),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX operator_sessions_user_id_idx ON operator_sessions(user_id);
CREATE INDEX operator_sessions_expires_at_idx ON operator_sessions(expires_at);

CREATE TABLE auth_audit (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor_user_id uuid NOT NULL REFERENCES users(id),
    actor_email text NOT NULL,
    actor_role text NOT NULL CHECK (actor_role IN ('admin', 'viewer')),
    action text NOT NULL CHECK (action IN (
        'organization.created', 'organization.viewer_joined', 'organization.join_code_rotated',
        'session.created', 'session.revoked'
    )),
    resource_kind text NOT NULL CHECK (resource_kind IN ('organization', 'session')),
    resource_id uuid NOT NULL,
    details jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (details = '{}'::jsonb)
);
CREATE INDEX auth_audit_organization_time_idx ON auth_audit(organization_id, occurred_at, id);

CREATE TABLE auth_changes (
    cursor bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    type text NOT NULL CHECK (type = 'audit.created'),
    data jsonb NOT NULL
);
CREATE INDEX auth_changes_organization_cursor_idx ON auth_changes(organization_id, cursor);

-- Legacy refresh_sessions remains for safe rollback; new auth never reads it.

-- +goose Down
DROP TABLE auth_changes;
DROP TABLE auth_audit;
DROP TABLE operator_sessions;
ALTER TABLE organizations DROP CONSTRAINT organizations_name_check;
ALTER TABLE organizations ADD CONSTRAINT organizations_name_check CHECK (char_length(name) BETWEEN 2 AND 120 AND name = btrim(name));
ALTER TABLE organizations DROP COLUMN join_code, DROP COLUMN join_code_revision, DROP COLUMN join_code_rotated_at;
ALTER TABLE users DROP COLUMN role;
