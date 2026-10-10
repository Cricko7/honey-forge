-- +goose Up
-- PostgreSQL timestamps retain microseconds; keep the remaining nanoseconds
-- separately so RFC3339Nano sorting and keyset pagination remain exact.
ALTER TABLE trap_events ADD COLUMN occurred_at_submicro smallint NOT NULL DEFAULT 0
    CHECK (occurred_at_submicro BETWEEN 0 AND 999);
UPDATE trap_events SET occurred_at_submicro =
    substring(rpad(COALESCE((regexp_match(envelope->>'occurred_at', '\.([0-9]+)Z$'))[1], ''), 9, '0') from 7 for 3)::smallint;
DROP INDEX trap_events_page;
DROP INDEX trap_events_trap;
CREATE INDEX trap_events_page ON trap_events(organization_id,occurred_at DESC,occurred_at_submicro DESC,event_id DESC);
CREATE INDEX trap_events_trap ON trap_events(organization_id,trap_id,occurred_at DESC,occurred_at_submicro DESC,event_id DESC);

-- +goose Down
DROP INDEX trap_events_page;
DROP INDEX trap_events_trap;
ALTER TABLE trap_events DROP COLUMN occurred_at_submicro;
CREATE INDEX trap_events_page ON trap_events(organization_id,occurred_at DESC,event_id DESC);
CREATE INDEX trap_events_trap ON trap_events(organization_id,trap_id,occurred_at DESC,event_id DESC);
