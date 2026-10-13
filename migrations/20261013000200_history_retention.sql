-- +goose Up
CREATE INDEX trap_events_retention ON trap_events(received_at);
CREATE INDEX trap_event_batches_retention ON trap_event_batches(stored_at);
CREATE INDEX commands_retention ON commands(finished_at) WHERE status IN ('succeeded','failed','expired');
CREATE INDEX auth_audit_retention ON auth_audit(occurred_at);
CREATE INDEX profile_audit_retention ON profile_audit(occurred_at);
CREATE INDEX mutation_audit_retention ON mutation_audit(created_at);
CREATE INDEX auth_changes_retention ON auth_changes(occurred_at);
CREATE INDEX profile_changes_retention ON profile_changes(occurred_at);

-- +goose Down
DROP INDEX profile_changes_retention;
DROP INDEX auth_changes_retention;
DROP INDEX mutation_audit_retention;
DROP INDEX profile_audit_retention;
DROP INDEX auth_audit_retention;
DROP INDEX commands_retention;
DROP INDEX trap_events_retention;
DROP INDEX trap_event_batches_retention;
