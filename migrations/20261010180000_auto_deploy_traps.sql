-- +goose Up
ALTER TABLE traps ADD COLUMN auto_deploy boolean NOT NULL DEFAULT false;
ALTER TABLE traps ADD COLUMN auto_deploy_actor_id uuid;
ALTER TABLE traps ADD CONSTRAINT auto_deploy_actor_required CHECK (NOT auto_deploy OR auto_deploy_actor_id IS NOT NULL);

-- +goose Down
ALTER TABLE traps DROP CONSTRAINT auto_deploy_actor_required;
ALTER TABLE traps DROP COLUMN auto_deploy;
ALTER TABLE traps DROP COLUMN auto_deploy_actor_id;
