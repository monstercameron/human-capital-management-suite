-- +goose Up
-- time_observation is the immutable evidence for one observed punch: server
-- receipt time is stamped by the row's own created_at/received_at (never
-- client-supplied), and corrects_id links a later correction to the earlier
-- observation it corrects without ever rewriting that earlier row.
-- PERMANENT: the forbid_mutation trigger makes that a schema-level fact, not
-- a convention.
CREATE TABLE time_observation (
 tenant_id text NOT NULL, id text NOT NULL,
 worker_ref text NOT NULL, assignment_ref text NOT NULL, device_ref text NOT NULL DEFAULT '',
 source text NOT NULL, event_type text NOT NULL, project_ref text NOT NULL DEFAULT '',
 timezone text NOT NULL DEFAULT '',
 occurred_at timestamptz NOT NULL, received_at timestamptz NOT NULL,
 idempotency_key text NOT NULL, digest text NOT NULL,
 corrects_id text,
 payload jsonb NOT NULL DEFAULT '{}'::jsonb,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, id),
 UNIQUE (tenant_id, source, idempotency_key),
 FOREIGN KEY (tenant_id, corrects_id) REFERENCES time_observation(tenant_id, id)
);
CREATE INDEX time_observation_worker_range ON time_observation(tenant_id, worker_ref, occurred_at, id);
CREATE INDEX time_observation_corrects ON time_observation(tenant_id, corrects_id) WHERE corrects_id IS NOT NULL;
CREATE TRIGGER time_observation_immutable BEFORE UPDATE OR DELETE ON time_observation FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- +goose StatementBegin
SELECT time_enable_tenant_isolation('time_observation');
-- +goose StatementEnd

-- +goose Down
DROP TABLE time_observation CASCADE;
