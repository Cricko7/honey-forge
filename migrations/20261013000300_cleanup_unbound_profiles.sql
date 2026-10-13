-- +goose Up
-- Serialize with bindings and profile edits during a rolling deployment.
LOCK TABLE organization_changes IN EXCLUSIVE MODE;
LOCK TABLE profiles, traps IN SHARE ROW EXCLUSIVE MODE;
WITH removed AS (
    UPDATE profiles p SET deleted_at=clock_timestamp()
    WHERE p.deleted_at IS NULL AND NOT EXISTS (
        SELECT 1 FROM traps t
        WHERE t.organization_id=p.organization_id AND t.profile_id=p.id AND t.deleted_at IS NULL
    )
    RETURNING p.organization_id,p.id
)
INSERT INTO profile_changes(organization_id,type,data)
SELECT organization_id,'profile.deleted',jsonb_build_object('profile_id',id) FROM removed;

-- +goose Down
-- Cleanup retains revisions and request IDs; rollback cannot restore deleted profiles.
SELECT 1;
