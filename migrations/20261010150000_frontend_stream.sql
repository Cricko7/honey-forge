-- +goose Up
-- Existing journals lack historical DTOs. A fresh REST snapshot can resume from
-- this floor; older tokens must resynchronize instead of receiving invented data.
ALTER TABLE organization_changes ADD COLUMN stream_floor bigint NOT NULL DEFAULT 0;
UPDATE organization_changes SET stream_floor=sequence;
ALTER TABLE mutation_changes ADD COLUMN data jsonb;
CREATE INDEX mutation_changes_retention ON mutation_changes(organization_id,created_at,sequence);

-- Explicit DTO allowlists: never serialize storage records or configurations.
-- +goose StatementBegin
CREATE FUNCTION frontend_change_data(kind text, org uuid, resource uuid, metadata jsonb)
RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE payload jsonb;
BEGIN
    CASE kind
    WHEN 'event.created' THEN RETURN jsonb_build_object('event',metadata);
    WHEN 'trap.deleted' THEN RETURN jsonb_build_object('trap_id',resource);
    WHEN 'trap.changed' THEN
        SELECT jsonb_build_object('trap',to_jsonb(dto)) INTO payload FROM (
            SELECT id,name,description,profile_id,type_id,type_version,interaction_level,
                revision,state_version,created_at,updated_at,connectivity,last_seen_at,
                runtime_state,desired_state,desired_profile_revision,applied_profile_revision,
                agent,active_command_id
            FROM traps WHERE organization_id=org AND id=resource
        ) dto;
    WHEN 'command.changed' THEN
        SELECT jsonb_build_object('trap_id',dto.trap_id,'command',to_jsonb(dto)) INTO payload FROM (
            SELECT id,trap_id,request_id,action,params,status,target_profile_revision,
                created_at,expires_at,started_at,finished_at,result,error
            FROM commands WHERE organization_id=org AND id=resource
        ) dto;
    ELSE RETURN metadata;
    END CASE;
    RETURN payload;
END;
$$;
-- +goose StatementEnd

-- Snapshot inside the business transaction, before later changes can overwrite
-- state_version/status. All existing feature writers use this same journal.
-- +goose StatementBegin
CREATE FUNCTION snapshot_frontend_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.data IS NULL THEN
        NEW.data := frontend_change_data(NEW.type,NEW.organization_id,NEW.resource_id,NEW.metadata);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER mutation_changes_snapshot BEFORE INSERT ON mutation_changes
FOR EACH ROW EXECUTE FUNCTION snapshot_frontend_change();

-- Keep module 04's existing local journal and atomically bridge its notifications
-- into the same organization commit order used by events/traps/commands.
-- +goose StatementBegin
CREATE FUNCTION publish_profile_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE next_sequence bigint; resource uuid;
BEGIN
    INSERT INTO organization_changes(organization_id) VALUES(NEW.organization_id) ON CONFLICT DO NOTHING;
    UPDATE organization_changes SET sequence=sequence+1 WHERE organization_id=NEW.organization_id RETURNING sequence INTO next_sequence;
    resource := COALESCE(NEW.data->>'profile_id',NEW.data->>'audit_id')::uuid;
    INSERT INTO mutation_changes(organization_id,sequence,type,resource_id,metadata,data,created_at)
    VALUES(NEW.organization_id,next_sequence,NEW.type,resource,'{}',NEW.data,clock_timestamp());
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER profile_changes_frontend AFTER INSERT ON profile_changes
FOR EACH ROW EXECUTE FUNCTION publish_profile_change();

CREATE TABLE frontend_catalog_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    etag text NOT NULL
);

-- +goose Down
DROP TABLE frontend_catalog_state;
DROP TRIGGER profile_changes_frontend ON profile_changes;
DROP FUNCTION publish_profile_change();
DROP TRIGGER mutation_changes_snapshot ON mutation_changes;
DROP FUNCTION snapshot_frontend_change();
DROP FUNCTION frontend_change_data(text,uuid,uuid,jsonb);
DROP INDEX mutation_changes_retention;
ALTER TABLE mutation_changes DROP COLUMN data;
ALTER TABLE organization_changes DROP COLUMN stream_floor;
