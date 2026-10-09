-- +goose Up
CREATE TABLE profiles (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    type_id text NOT NULL CHECK (type_id ~ '^[a-z][a-z0-9_.-]{0,63}$'),
    type_version integer NOT NULL CHECK (type_version >= 1),
    interaction_level text NOT NULL CHECK (char_length(interaction_level) BETWEEN 1 AND 32),
    revision integer NOT NULL CHECK (revision >= 1),
    created_at timestamptz NOT NULL,
    deleted_at timestamptz,
    UNIQUE (organization_id, id)
);
CREATE INDEX profiles_organization_page_idx ON profiles(organization_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX profiles_organization_type_page_idx ON profiles(organization_id, type_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;

CREATE TABLE profile_revisions (
    profile_id uuid NOT NULL REFERENCES profiles(id),
    revision integer NOT NULL CHECK (revision >= 1),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100 AND name = btrim(name)),
    description text NOT NULL CHECK (char_length(description) <= 1000),
    config json NOT NULL CHECK (json_typeof(config) = 'object' AND octet_length(config::text) <= 131072),
    secret_fields_set jsonb NOT NULL CHECK (jsonb_typeof(secret_fields_set) = 'array'),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY(profile_id, revision)
);
ALTER TABLE profiles ADD CONSTRAINT profiles_current_revision_fkey
    FOREIGN KEY(id, revision) REFERENCES profile_revisions(profile_id, revision) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE profile_requests (
    organization_id uuid NOT NULL REFERENCES organizations(id),
    request_id uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    profile_id uuid NOT NULL,
    PRIMARY KEY(organization_id, request_id),
    FOREIGN KEY(organization_id, profile_id) REFERENCES profiles(organization_id, id)
);

CREATE TABLE profile_audit (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor_user_id uuid NOT NULL REFERENCES users(id),
    actor_email text NOT NULL,
    actor_role text NOT NULL CHECK (actor_role IN ('admin', 'viewer')),
    action text NOT NULL CHECK (action IN ('profile.created', 'profile.updated', 'profile.deleted')),
    resource_id uuid NOT NULL REFERENCES profiles(id),
    details jsonb NOT NULL CHECK (jsonb_typeof(details) = 'object' AND details - 'changed_fields' - 'profile_revision' = '{}'::jsonb)
);
CREATE INDEX profile_audit_organization_time_idx ON profile_audit(organization_id, occurred_at DESC, id DESC);

CREATE TABLE profile_changes (
    cursor bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    type text NOT NULL CHECK (type IN ('profile.changed', 'profile.deleted', 'audit.created')),
    data jsonb NOT NULL CHECK (jsonb_typeof(data) = 'object')
);
CREATE INDEX profile_changes_organization_cursor_idx ON profile_changes(organization_id, cursor);

-- +goose Down
DROP TABLE profile_changes;
DROP TABLE profile_audit;
DROP TABLE profile_requests;
ALTER TABLE profiles DROP CONSTRAINT profiles_current_revision_fkey;
DROP TABLE profile_revisions;
DROP TABLE profiles;
