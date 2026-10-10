-- +goose Up
ALTER TABLE mutation_audit ADD COLUMN actor_email text NOT NULL DEFAULT '';
ALTER TABLE mutation_audit ADD COLUMN actor_role text NOT NULL DEFAULT 'admin' CHECK (actor_role IN ('admin','viewer'));
ALTER TABLE mutation_audit ADD COLUMN resource_kind text NOT NULL DEFAULT 'trap';
ALTER TABLE mutation_audit ADD COLUMN details jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(details)='object');
-- Older mutation rows did not record actor snapshots. Backfill the available
-- identity once; future reads never join mutable users or reinterpret metadata.
UPDATE mutation_audit a SET actor_email=u.email,actor_role=u.role FROM users u WHERE a.actor_id=u.id;
UPDATE mutation_audit SET action=CASE action WHEN 'agent_credentials.issued' THEN 'trap.agent_credentials_issued' WHEN 'agent_credentials.revoked' THEN 'trap.agent_credentials_revoked' ELSE action END,
 resource_kind=split_part(action,'.',1);
UPDATE mutation_audit SET resource_kind='trap' WHERE resource_kind='agent_credentials';
CREATE VIEW audit_entries AS
 SELECT id,organization_id,occurred_at,actor_user_id AS actor_id,actor_email,actor_role,action,resource_kind,resource_id,details FROM auth_audit
 UNION ALL SELECT id,organization_id,occurred_at,actor_user_id,actor_email,actor_role,action,'profile',resource_id,details FROM profile_audit
 UNION ALL SELECT id,organization_id,created_at,actor_id,actor_email,actor_role,action,resource_kind,resource_id,details FROM mutation_audit;

-- Auth's local journal remains compatible with old consumers; the shared stream
-- publishes only the public audit_id DTO, under the organization commit lock.
-- +goose StatementBegin
CREATE FUNCTION publish_auth_audit_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE next_sequence bigint; audit_id uuid;
BEGIN
    audit_id := COALESCE(NEW.data->>'audit_id',NEW.data->>'id')::uuid;
    IF audit_id IS NULL THEN RAISE EXCEPTION 'auth audit id missing'; END IF;
    INSERT INTO organization_changes(organization_id) VALUES(NEW.organization_id) ON CONFLICT DO NOTHING;
    UPDATE organization_changes SET sequence=sequence+1 WHERE organization_id=NEW.organization_id RETURNING sequence INTO next_sequence;
    INSERT INTO mutation_changes(organization_id,sequence,type,resource_id,metadata,data)
    VALUES(NEW.organization_id,next_sequence,'audit.created',audit_id,'{}',jsonb_build_object('audit_id',audit_id));
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER auth_changes_frontend AFTER INSERT ON auth_changes FOR EACH ROW EXECUTE FUNCTION publish_auth_audit_change();

-- +goose StatementBegin
CREATE FUNCTION snapshot_audit_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.type='audit.created' THEN NEW.data:=jsonb_build_object('audit_id',NEW.resource_id); END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER mutation_audit_snapshot BEFORE INSERT ON mutation_changes FOR EACH ROW EXECUTE FUNCTION snapshot_audit_change();

-- +goose Down
DROP TRIGGER mutation_audit_snapshot ON mutation_changes;
DROP FUNCTION snapshot_audit_change();
DROP TRIGGER auth_changes_frontend ON auth_changes;
DROP FUNCTION publish_auth_audit_change();
DROP VIEW audit_entries;
ALTER TABLE mutation_audit DROP COLUMN details,DROP COLUMN resource_kind,DROP COLUMN actor_role,DROP COLUMN actor_email;
