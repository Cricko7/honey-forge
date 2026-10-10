-- +goose Up
CREATE TABLE commands (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    trap_id uuid NOT NULL,
    request_id uuid NOT NULL,
    action text NOT NULL,
    params jsonb NOT NULL CHECK (jsonb_typeof(params) = 'object'),
    status text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'expired')),
    target_profile_revision integer CHECK (target_profile_revision > 0),
    configuration jsonb CHECK (configuration IS NULL OR jsonb_typeof(configuration) = 'object'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    started_at timestamptz,
    finished_at timestamptz,
    result jsonb CHECK (result IS NULL OR jsonb_typeof(result) = 'object'),
    error jsonb CHECK (error IS NULL OR jsonb_typeof(error) = 'object'),
    lease_id uuid,
    lease_expires_at timestamptz,
    UNIQUE (organization_id, trap_id, request_id),
    CHECK ((action = 'apply_config') = (target_profile_revision IS NOT NULL AND configuration IS NOT NULL)),
    CHECK (
        (status = 'queued' AND started_at IS NULL AND finished_at IS NULL AND result IS NULL AND error IS NULL AND lease_id IS NULL) OR
        (status = 'running' AND started_at IS NOT NULL AND finished_at IS NULL AND result IS NULL AND error IS NULL AND lease_id IS NOT NULL) OR
        (status = 'succeeded' AND finished_at IS NOT NULL AND result IS NOT NULL AND error IS NULL) OR
        (status IN ('failed', 'expired') AND finished_at IS NOT NULL AND result IS NULL AND error IS NOT NULL)
    )
);
CREATE UNIQUE INDEX commands_one_active_per_trap ON commands(trap_id) WHERE status IN ('queued', 'running');
CREATE INDEX commands_history ON commands(organization_id, trap_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE commands;
