-- +goose Up
CREATE TABLE catalog_versions (
    type_id text NOT NULL CHECK (type_id ~ '^[a-z][a-z0-9_.-]{0,63}$'),
    type_version integer NOT NULL CHECK (type_version >= 1),
    entry jsonb NOT NULL CHECK (jsonb_typeof(entry) = 'object'),
    PRIMARY KEY (type_id, type_version),
    CHECK (entry->>'type_id' IS NOT NULL AND entry->>'type_id' = type_id),
    CHECK (entry->>'type_version' IS NOT NULL AND (entry->>'type_version')::integer = type_version)
);
CREATE TRIGGER catalog_versions_immutable BEFORE UPDATE OR DELETE ON catalog_versions
    FOR EACH ROW EXECUTE FUNCTION prevent_schema_version_change();

-- +goose Down
DROP TABLE catalog_versions;
