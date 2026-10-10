-- +goose Up
ALTER TABLE trap_ingestions ADD COLUMN fingerprint bytea CHECK (octet_length(fingerprint)=32);
INSERT INTO trap_ingestions(trap_id,batch_id,finished,fingerprint)
SELECT trap_id,batch_id,true,fingerprint FROM trap_event_batches
ON CONFLICT(trap_id,batch_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint;

-- +goose Down
ALTER TABLE trap_ingestions DROP COLUMN fingerprint;
