-- WF-RUN-007: durable QuarantinedWork for poison node executions.
--
-- Until this migration runtime.QuarantineLedger was the only home of a
-- quarantined node: a mutex-guarded in-memory map that a restart emptied and
-- that the served driver never reached. An OBSERVE node whose retry budget
-- was exhausted with no failure route was refused NO_FAILURE_ROUTE, its
-- advance transaction rolled back, and nothing durable recorded the attempts,
-- the error or why the instance could not move.
--
-- workflow_quarantined_work retains one sealed record per poisoned node
-- execution, keyed by the tenant and the record's idempotency key: attempts,
-- last error, ambiguity, owner, SLA, next action, repair route and the route
-- the instance took (BLOCKED, REPAIR_REQUIRED or QUARANTINED). The driver
-- inserts it inside the same advance transaction that routes the instance, so
-- the record and the instance status commit together or not at all.
--
-- The row is an operations projection, never a terminal business outcome:
-- the instance status carries the workflow state, and a resolution is a later
-- governed transition, not an edit of this record. It is append-only: SELECT
-- and INSERT are granted, never UPDATE or DELETE, and forbid_mutation refuses
-- both even for a privileged role.

-- +goose Up

CREATE TABLE workflow_quarantined_work (
    tenant_id       tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    idempotency_key text        NOT NULL,
    instance_id     uuid        NOT NULL,
    node_id         text        NOT NULL,
    workflow_id     text        NOT NULL,
    attempts        integer     NOT NULL,
    last_error      text        NOT NULL,
    ambiguous       boolean     NOT NULL,
    owner           text        NOT NULL,
    sla_nanos       bigint      NOT NULL,
    next_action     text        NOT NULL,
    repair_route    text        NOT NULL,
    route           text        NOT NULL,
    record_digest   text        NOT NULL,
    recorded_at     timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, idempotency_key),
    FOREIGN KEY (tenant_id, instance_id) REFERENCES workflow_instance (tenant_id, instance_id),
    CONSTRAINT workflow_quarantined_work_route CHECK (route IN ('BLOCKED', 'REPAIR_REQUIRED', 'QUARANTINED')),
    CONSTRAINT workflow_quarantined_work_identity CHECK (idempotency_key <> '' AND node_id <> '' AND workflow_id <> ''),
    CONSTRAINT workflow_quarantined_work_attempts_positive CHECK (attempts > 0),
    CONSTRAINT workflow_quarantined_work_sla_nonnegative CHECK (sla_nanos >= 0),
    CONSTRAINT workflow_quarantined_work_next_action CHECK (next_action <> ''),
    CONSTRAINT workflow_quarantined_work_digest CHECK (record_digest LIKE 'sha256:%')
);

CREATE INDEX workflow_quarantined_work_by_instance
    ON workflow_quarantined_work (tenant_id, instance_id, recorded_at);

CREATE TRIGGER workflow_quarantined_work_append_only
    BEFORE UPDATE OR DELETE ON workflow_quarantined_work
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE workflow_quarantined_work ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_quarantined_work FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_quarantined_work
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON workflow_quarantined_work TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00297 is irreversible: migrations 00279-00296 already broke the rollback chain, so this migration keeps that true rather than claiming a reversibility no rollback can ever reach'; END $$;
-- +goose StatementEnd
