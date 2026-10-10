-- +goose Up
ALTER TABLE auth_audit ADD COLUMN stream_sequence bigint NOT NULL DEFAULT 0;
ALTER TABLE profile_audit ADD COLUMN stream_sequence bigint NOT NULL DEFAULT 0;
ALTER TABLE mutation_audit ADD COLUMN stream_sequence bigint NOT NULL DEFAULT 0;
UPDATE auth_audit a SET stream_sequence=c.sequence FROM mutation_changes c WHERE c.type='audit.created' AND c.resource_id=a.id AND c.organization_id=a.organization_id;
UPDATE profile_audit a SET stream_sequence=c.sequence FROM mutation_changes c WHERE c.type='audit.created' AND c.resource_id=a.id AND c.organization_id=a.organization_id;
UPDATE mutation_audit a SET stream_sequence=c.sequence FROM mutation_changes c WHERE c.type='audit.created' AND c.resource_id=a.id AND c.organization_id=a.organization_id;

-- The notification sequence is assigned under the common organization lock,
-- so late transactions cannot enter a previously captured REST page snapshot.
-- +goose StatementBegin
CREATE FUNCTION stamp_audit_sequence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE auth_audit SET stream_sequence=NEW.sequence WHERE id=NEW.resource_id AND organization_id=NEW.organization_id;
    UPDATE profile_audit SET stream_sequence=NEW.sequence WHERE id=NEW.resource_id AND organization_id=NEW.organization_id;
    UPDATE mutation_audit SET stream_sequence=NEW.sequence WHERE id=NEW.resource_id AND organization_id=NEW.organization_id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_commit_sequence AFTER INSERT ON mutation_changes FOR EACH ROW WHEN (NEW.type='audit.created') EXECUTE FUNCTION stamp_audit_sequence();
CREATE OR REPLACE VIEW audit_entries AS
 SELECT id,organization_id,occurred_at,actor_user_id AS actor_id,actor_email,actor_role,action,resource_kind,resource_id,details,stream_sequence FROM auth_audit
 UNION ALL SELECT id,organization_id,occurred_at,actor_user_id,actor_email,actor_role,action,'profile',resource_id,details,stream_sequence FROM profile_audit
 UNION ALL SELECT id,organization_id,created_at,actor_id,actor_email,actor_role,action,resource_kind,resource_id,details,stream_sequence FROM mutation_audit;

-- +goose Down
DROP TRIGGER audit_commit_sequence ON mutation_changes;
DROP FUNCTION stamp_audit_sequence();
DROP VIEW audit_entries;
CREATE VIEW audit_entries AS
 SELECT id,organization_id,occurred_at,actor_user_id AS actor_id,actor_email,actor_role,action,resource_kind,resource_id,details FROM auth_audit
 UNION ALL SELECT id,organization_id,occurred_at,actor_user_id,actor_email,actor_role,action,'profile',resource_id,details FROM profile_audit
 UNION ALL SELECT id,organization_id,created_at,actor_id,actor_email,actor_role,action,resource_kind,resource_id,details FROM mutation_audit;
ALTER TABLE mutation_audit DROP COLUMN stream_sequence;
ALTER TABLE profile_audit DROP COLUMN stream_sequence;
ALTER TABLE auth_audit DROP COLUMN stream_sequence;
