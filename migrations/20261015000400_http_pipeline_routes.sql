-- +goose Up
CREATE TABLE http_pipeline_routes (
    source_id uuid NOT NULL REFERENCES traps(id),
    request_id uuid NOT NULL,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    source_generation bigint NOT NULL,
    fingerprint bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (source_id, request_id, source_generation)
);
CREATE INDEX http_pipeline_routes_expiry ON http_pipeline_routes(organization_id, expires_at);

-- +goose Down
DROP TABLE http_pipeline_routes;
