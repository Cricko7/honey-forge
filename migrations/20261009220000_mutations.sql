-- +goose Up
CREATE TABLE mutation_requests (
    organization_id uuid NOT NULL,
    route text NOT NULL CHECK (route <> ''),
    trap_scope text NOT NULL DEFAULT '',
    request_id uuid NOT NULL,
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    resource_id uuid,
    location text,
    stream_sequence bigint,
    deleted boolean NOT NULL DEFAULT false,
    PRIMARY KEY (organization_id, route, trap_scope, request_id)
);

-- Serialize organization writes so stream sequence order is commit order.
CREATE TABLE organization_changes (
    organization_id uuid PRIMARY KEY,
    sequence bigint NOT NULL DEFAULT 0 CHECK (sequence >= 0)
);

CREATE TABLE mutation_audit (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    action text NOT NULL,
    resource_id uuid NOT NULL,
    metadata jsonb NOT NULL CHECK (jsonb_typeof(metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mutation_audit_org_idx ON mutation_audit(organization_id, created_at, id);

CREATE TABLE mutation_changes (
    organization_id uuid NOT NULL,
    sequence bigint NOT NULL CHECK (sequence > 0),
    type text NOT NULL,
    resource_id uuid NOT NULL,
    metadata jsonb NOT NULL CHECK (jsonb_typeof(metadata) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, sequence)
);

-- +goose Down
DROP TABLE mutation_changes;
DROP TABLE mutation_audit;
DROP TABLE organization_changes;
DROP TABLE mutation_requests;
