-- +goose Up
-- PostgreSQL jsonb rejects escaped NUL even though it is valid captured text.
-- Keep queryable metadata in jsonb and captured data in lossless JSON.
ALTER TABLE trap_events ADD COLUMN event_data json;
UPDATE trap_events SET event_data=(envelope->'data')::json, envelope=envelope-'data';
ALTER TABLE trap_events ALTER COLUMN event_data SET NOT NULL;
ALTER TABLE trap_events ADD CONSTRAINT trap_events_data_object CHECK (json_typeof(event_data)='object');

-- +goose Down
-- This conversion intentionally fails rather than losing data if NUL was captured.
UPDATE trap_events SET envelope=envelope || jsonb_build_object('data', event_data::jsonb);
ALTER TABLE trap_events DROP COLUMN event_data;
