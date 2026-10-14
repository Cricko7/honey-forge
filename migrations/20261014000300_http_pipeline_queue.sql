-- +goose Up
CREATE TABLE http_pipeline_traces (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    source_ip inet NOT NULL,
    source_port integer NOT NULL CHECK (source_port BETWEEN 1 AND 65535),
    hops integer NOT NULL DEFAULT 0 CHECK (hops BETWEEN 0 AND 4096),
    expires_at timestamptz NOT NULL
);
CREATE INDEX http_pipeline_traces_expiry ON http_pipeline_traces(expires_at);

CREATE TABLE http_pipeline_deliveries (
    id uuid PRIMARY KEY,
    trace_id uuid NOT NULL REFERENCES http_pipeline_traces(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    source_id uuid NOT NULL REFERENCES traps(id),
    target_id uuid NOT NULL REFERENCES traps(id),
    source_generation bigint NOT NULL,
    target_generation bigint NOT NULL,
    request_id uuid NOT NULL,
    fingerprint bytea NOT NULL,
    delivery jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','succeeded','failed','cancelled')),
    lease_until timestamptz NOT NULL DEFAULT '-infinity',
    attempts integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (source_id, request_id)
);
CREATE INDEX http_pipeline_pending ON http_pipeline_deliveries(target_id, lease_until, created_at) WHERE status='pending';

-- +goose Down
DROP TABLE http_pipeline_deliveries;
DROP TABLE http_pipeline_traces;
