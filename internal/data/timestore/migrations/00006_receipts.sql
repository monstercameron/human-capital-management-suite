-- +goose Up
-- time_receipt is the immutable per-punch receipt for one device batch
-- ingest: PRIMARY KEY(tenant_id, device_id, device_sequence) makes a
-- retried device_sequence resolve to its original row rather than a new
-- one, so RecordBatch can always return "the" receipt for a sequence.
-- PERMANENT and append-only.
CREATE TABLE time_receipt (
 tenant_id text NOT NULL, device_id text NOT NULL, device_sequence bigint NOT NULL CHECK (device_sequence > 0),
 status text NOT NULL, reason text NOT NULL DEFAULT '', observation_id text,
 payload jsonb NOT NULL DEFAULT '{}'::jsonb,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, device_id, device_sequence)
);
CREATE TRIGGER time_receipt_immutable BEFORE UPDATE OR DELETE ON time_receipt FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- time_receipt_cursor is REBUILDABLE serving state derived from time_receipt:
-- the highest device_sequence contiguously acknowledged from 1, maintained
-- transactionally alongside each RecordBatch so a reader never has to walk
-- the receipt table to trim a device queue.
CREATE TABLE time_receipt_cursor (
 tenant_id text NOT NULL, device_id text NOT NULL,
 highest_contiguous bigint NOT NULL DEFAULT 0 CHECK (highest_contiguous >= 0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, device_id)
);

-- +goose StatementBegin
SELECT time_enable_tenant_isolation('time_receipt');
-- +goose StatementEnd
-- +goose StatementBegin
SELECT time_enable_tenant_isolation('time_receipt_cursor');
-- +goose StatementEnd

-- +goose Down
DROP TABLE time_receipt_cursor CASCADE;
DROP TABLE time_receipt CASCADE;
