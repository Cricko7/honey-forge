-- +goose Up
CREATE TABLE schema_versions (
    type_id text NOT NULL CHECK (type_id ~ '^[a-z][a-z0-9_.-]{0,63}$'),
    type_version integer NOT NULL CHECK (type_version >= 1),
    schema jsonb NOT NULL,
    PRIMARY KEY (type_id, type_version)
);
-- Versioned schemas are insert-only even if a later repository tries to update them.
-- +goose StatementBegin
CREATE FUNCTION prevent_schema_version_change() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'schema versions are immutable';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER schema_versions_immutable BEFORE UPDATE OR DELETE ON schema_versions
    FOR EACH ROW EXECUTE FUNCTION prevent_schema_version_change();

-- +goose Down
DROP TABLE schema_versions;
DROP FUNCTION prevent_schema_version_change();
