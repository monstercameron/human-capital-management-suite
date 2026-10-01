-- +goose Up
-- FTIME-005 persistence: approved-time allocation rows keyed to the
-- timecard revision they were approved at. Append-only: a correction never
-- rewrites an existing allocation row, it allocates a delta against the
-- corrected timecard's new revision. Idempotent per allocation revision:
-- the same (timecard, timecard_revision, allocation_key) with the same
-- payload digest always returns the original row; a different digest is a
-- conflict.

CREATE TABLE work_order_allocation (
 tenant_id text NOT NULL, id text NOT NULL,
 timecard_id text NOT NULL, timecard_revision bigint NOT NULL CHECK (timecard_revision > 0),
 allocation_key text NOT NULL CHECK (allocation_key <> ''),
 work_order_ref text NOT NULL, project_ref text NOT NULL,
 minutes bigint NOT NULL CHECK (minutes >= 0),
 source_ref text NOT NULL, payload_digest text NOT NULL CHECK (payload_digest <> ''),
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, timecard_id, timecard_revision, allocation_key)
);
CREATE INDEX work_order_allocation_by_order ON work_order_allocation(tenant_id, work_order_ref);
CREATE TRIGGER work_order_allocation_immutable BEFORE UPDATE OR DELETE ON work_order_allocation
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

SELECT time_enable_tenant_isolation('work_order_allocation');

-- +goose Down
DROP TABLE work_order_allocation CASCADE;
