-- +goose Up
CREATE TABLE trap_events (
    event_id uuid PRIMARY KEY,
    trap_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    session_id uuid NOT NULL,
    session_sequence bigint NOT NULL CHECK (session_sequence BETWEEN 1 AND 9007199254740991),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint)=32),
    envelope jsonb NOT NULL CHECK (jsonb_typeof(envelope)='object'),
    UNIQUE (trap_id,session_id,session_sequence),
    FOREIGN KEY (organization_id,trap_id) REFERENCES traps(organization_id,id)
);
CREATE INDEX trap_events_page ON trap_events(organization_id,occurred_at DESC,event_id DESC);
CREATE INDEX trap_events_trap ON trap_events(organization_id,trap_id,occurred_at DESC,event_id DESC);
CREATE TABLE trap_event_batches (
    trap_id uuid NOT NULL REFERENCES traps(id),
    batch_id uuid NOT NULL,
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint)=32),
    stored_at timestamptz NOT NULL,
    PRIMARY KEY(trap_id,batch_id)
);
-- +goose Down
DROP TABLE trap_event_batches;
DROP TABLE trap_events;
