-- +goose Up
CREATE TABLE trap_links (
    organization_id uuid NOT NULL REFERENCES organizations(id),
    source_id uuid NOT NULL,
    target_id uuid NOT NULL,
    PRIMARY KEY (organization_id, source_id, target_id),
    FOREIGN KEY (organization_id, source_id) REFERENCES traps(organization_id, id),
    FOREIGN KEY (organization_id, target_id) REFERENCES traps(organization_id, id),
    CHECK (source_id <> target_id)
);

-- +goose Down
DROP TABLE trap_links;
