-- +goose Up
CREATE TABLE traps (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    profile_id uuid NOT NULL,
    type_id text NOT NULL,
    type_version integer NOT NULL CHECK (type_version >= 1),
    interaction_level text NOT NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100 AND name = btrim(name)),
    description text NOT NULL CHECK (char_length(description) <= 1000),
    revision integer NOT NULL DEFAULT 1 CHECK (revision >= 1),
    state_version integer NOT NULL DEFAULT 1 CHECK (state_version >= 1),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    connectivity text NOT NULL DEFAULT 'offline' CHECK (connectivity IN ('online','offline')),
    last_seen_at timestamptz,
    runtime_state text NOT NULL DEFAULT 'unknown' CHECK (runtime_state IN ('unknown','running','stopped','error')),
    desired_state text NOT NULL DEFAULT 'stopped' CHECK (desired_state IN ('running','stopped')),
    desired_profile_revision integer CHECK (desired_profile_revision > 0),
    applied_profile_revision integer CHECK (applied_profile_revision > 0),
    agent jsonb CHECK (agent IS NULL OR (
        jsonb_typeof(agent) = 'object' AND
        char_length(agent->>'agent_version') BETWEEN 1 AND 64 AND
        char_length(agent->>'hostname') BETWEEN 1 AND 255 AND
        (agent->>'buffered_events')::bigint >= 0 AND
        (agent->>'buffer_bytes')::bigint >= 0 AND
        (agent->>'buffer_capacity_bytes')::bigint > 0 AND
        agent->>'buffer_state' IN ('ok','full','unavailable')
    )),
    active_command_id uuid REFERENCES commands(id) DEFERRABLE INITIALLY DEFERRED,
    generation integer NOT NULL DEFAULT 0 CHECK (generation >= 0),
    token_hash bytea UNIQUE CHECK (token_hash IS NULL OR octet_length(token_hash) = 32),
    issued_at timestamptz,
    connection_id uuid,
    applied_configuration jsonb,
    initial_trap jsonb NOT NULL CHECK (jsonb_typeof(initial_trap) = 'object'),
    deleted_at timestamptz,
    UNIQUE (organization_id,id),
    FOREIGN KEY (organization_id,profile_id) REFERENCES profiles(organization_id,id),
    FOREIGN KEY (type_id,type_version) REFERENCES catalog_versions(type_id,type_version),
    CHECK (connectivity = 'offline' OR (connection_id IS NOT NULL AND last_seen_at IS NOT NULL AND agent IS NOT NULL)),
    CHECK (token_hash IS NULL OR (generation > 0 AND issued_at IS NOT NULL)),
    CHECK (deleted_at IS NULL OR (token_hash IS NULL AND connection_id IS NULL AND connectivity = 'offline'))
);
CREATE INDEX traps_page ON traps(organization_id,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX traps_profile ON traps(organization_id,profile_id) WHERE deleted_at IS NULL;
CREATE INDEX traps_connections ON traps(last_seen_at) WHERE connection_id IS NOT NULL AND deleted_at IS NULL;
ALTER TABLE commands ADD CONSTRAINT commands_trap_owner_fkey
    FOREIGN KEY (organization_id,trap_id) REFERENCES traps(organization_id,id)
    DEFERRABLE INITIALLY DEFERRED;

-- Module 07 retains a reservation until persistence has definitively finished.
-- An ambiguous timeout must not release it; reconciliation calls FinishIngestion.
CREATE TABLE trap_ingestions (
    trap_id uuid NOT NULL REFERENCES traps(id),
    batch_id uuid NOT NULL,
    finished boolean NOT NULL DEFAULT false,
    PRIMARY KEY(trap_id,batch_id)
);

-- +goose Down
DROP TABLE trap_ingestions;
ALTER TABLE commands DROP CONSTRAINT commands_trap_owner_fkey;
DROP TABLE traps;
