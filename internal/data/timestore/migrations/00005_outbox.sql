-- +goose Up
-- time_outbox is the clock/timecard event outbox: every row is written in
-- the same transaction as the state change it describes (sessions.go,
-- observations.go and receipts.go all insert here), and sequence is a
-- globally increasing identity so ListEvents(tenant, afterCursor, limit) is
-- an ordered, stable, resumable per-tenant scan. PERMANENT and append-only.
CREATE TABLE time_outbox (
 tenant_id text NOT NULL, id text NOT NULL,
 sequence bigint GENERATED ALWAYS AS IDENTITY,
 event_type text NOT NULL, schema_version integer NOT NULL CHECK (schema_version > 0),
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, sequence)
);
CREATE INDEX time_outbox_cursor ON time_outbox(tenant_id, sequence);
CREATE TRIGGER time_outbox_immutable BEFORE UPDATE OR DELETE ON time_outbox FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- +goose StatementBegin
SELECT time_enable_tenant_isolation('time_outbox');
-- +goose StatementEnd

-- +goose Down
DROP TABLE time_outbox CASCADE;
