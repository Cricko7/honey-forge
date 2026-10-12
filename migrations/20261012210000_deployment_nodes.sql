-- +goose Up
CREATE TABLE deployment_nodes (
    id text PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    hostname text NOT NULL,
    address text NOT NULL,
    ready boolean NOT NULL,
    observed_at timestamptz NOT NULL
);
ALTER TABLE traps ADD COLUMN deployment_node_id text NOT NULL DEFAULT '';
ALTER TABLE traps ADD CONSTRAINT deployment_requires_managed CHECK (deployment_node_id = '' OR auto_deploy);

-- +goose Down
ALTER TABLE traps DROP CONSTRAINT deployment_requires_managed;
ALTER TABLE traps DROP COLUMN deployment_node_id;
DROP TABLE deployment_nodes;
