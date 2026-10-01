-- +goose Up
-- Durable consumer progress and leases for the clock workflow outbox.
CREATE TABLE time_workflow_dispatch_cursor (
    tenant_id text NOT NULL,
    consumer text NOT NULL,
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    lease_owner text NOT NULL DEFAULT '',
    lease_until timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, consumer)
);
SELECT time_enable_tenant_isolation('time_workflow_dispatch_cursor');

-- Immutable binding from one authoritative clock session to its workflow run.
CREATE TABLE time_workflow_session_run (
    tenant_id text NOT NULL,
    session_id text NOT NULL,
    instance_id uuid NOT NULL,
    workflow_id text NOT NULL,
    plan_digest text NOT NULL,
    start_key text NOT NULL,
    correlation_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, session_id),
    UNIQUE (tenant_id, start_key)
);
CREATE INDEX time_workflow_session_run_instance_idx ON time_workflow_session_run (tenant_id, instance_id);
CREATE TRIGGER time_workflow_session_run_immutable BEFORE UPDATE OR DELETE ON time_workflow_session_run FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();
SELECT time_enable_tenant_isolation('time_workflow_session_run');

-- +goose Down
DROP TABLE time_workflow_session_run CASCADE;
DROP TABLE time_workflow_dispatch_cursor CASCADE;
