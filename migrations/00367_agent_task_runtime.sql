-- AGENT2-010/011/013: durable agent task runtime. A task row carries the state
-- machine, the confirmed plan (with its digest) and the context ledger; a wake
-- receipt row is the dedupe inbox that makes a delivered wake event idempotent.

-- +goose Up

CREATE TABLE agent_task (
    tenant_id        uuid        NOT NULL REFERENCES tenant(tenant_id),
    task_id          text        NOT NULL CHECK (btrim(task_id) <> ''),
    tenant_ref       text        NOT NULL CHECK (btrim(tenant_ref) <> ''),
    user_id          text        NOT NULL CHECK (btrim(user_id) <> ''),
    goal             text        NOT NULL,
    constraints      jsonb       NOT NULL CHECK (jsonb_typeof(constraints) IN ('array', 'null')),
    plan             jsonb       NOT NULL CHECK (jsonb_typeof(plan) = 'object'),
    plan_digest      text        NOT NULL,
    plan_confirmed   boolean     NOT NULL,
    state            text        NOT NULL CHECK (state IN (
                        'DRAFTING', 'AWAITING_PLAN_CONFIRMATION', 'RUNNING', 'WAITING',
                        'AWAITING_APPROVAL', 'PAUSED', 'COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED')),
    version          bigint      NOT NULL CHECK (version > 0),
    current_step     integer     NOT NULL CHECK (current_step >= 0),
    wake             jsonb       CHECK (wake IS NULL OR jsonb_typeof(wake) = 'object'),
    wake_kind        text        CHECK (wake_kind IN ('APPROVAL', 'SIGNAL', 'TIMER', 'USER_REPLY', 'POLLING')),
    wake_key         text        CHECK (wake_key IS NULL OR btrim(wake_key) <> ''),
    wake_due_at      timestamptz,
    wake_stale_after timestamptz,
    paused_state     text        NOT NULL DEFAULT '' CHECK (paused_state IN (
                        '', 'DRAFTING', 'AWAITING_PLAN_CONFIRMATION', 'RUNNING', 'WAITING',
                        'AWAITING_APPROVAL', 'PAUSED', 'COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED')),
    paused_wake      jsonb       CHECK (paused_wake IS NULL OR jsonb_typeof(paused_wake) = 'object'),
    worker_lease     text        NOT NULL DEFAULT '',
    model_session    text        NOT NULL DEFAULT '',
    failure_code     text        NOT NULL DEFAULT '',
    failure_detail   text        NOT NULL DEFAULT '',
    ledger           jsonb       NOT NULL CHECK (jsonb_typeof(ledger) = 'object'),
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    expires_at       timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, task_id),
    CHECK ((wake IS NULL) = (wake_kind IS NULL) AND (wake IS NULL) = (wake_key IS NULL)),
    CHECK (wake_due_at IS NULL OR wake_kind IS NOT NULL),
    CHECK (wake_stale_after IS NULL OR wake_kind IS NOT NULL)
);

-- Parked waits are found by their condition, never by scanning every task.
CREATE INDEX agent_task_due_timer ON agent_task (tenant_id, wake_due_at)
    WHERE wake_kind = 'TIMER' AND state = 'WAITING';
CREATE INDEX agent_task_stale_wake ON agent_task (tenant_id, wake_stale_after)
    WHERE wake_stale_after IS NOT NULL AND state IN ('WAITING', 'AWAITING_APPROVAL');
CREATE INDEX agent_task_open_expiry ON agent_task (tenant_id, expires_at)
    WHERE state NOT IN ('COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED');
CREATE INDEX agent_task_wake_key ON agent_task (tenant_id, wake_kind, wake_key)
    WHERE wake_kind IS NOT NULL;

CREATE TABLE agent_task_wake_receipt (
    tenant_id   uuid        NOT NULL REFERENCES tenant(tenant_id),
    task_id     text        NOT NULL,
    event_id    text        NOT NULL CHECK (btrim(event_id) <> ''),
    kind        text        NOT NULL CHECK (kind IN ('APPROVAL', 'SIGNAL', 'TIMER', 'USER_REPLY', 'POLLING')),
    wake_key    text        NOT NULL CHECK (btrim(wake_key) <> ''),
    correlation text        NOT NULL DEFAULT '',
    payload_ref text        NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL,
    outcome     text        NOT NULL CHECK (outcome IN ('ACCEPTED', 'IGNORED')),
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, task_id, event_id),
    FOREIGN KEY (tenant_id, task_id) REFERENCES agent_task (tenant_id, task_id)
);

ALTER TABLE agent_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_task FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_task
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE agent_task_wake_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_task_wake_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_task_wake_receipt
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_task_wake_receipt_immutable
    BEFORE UPDATE OR DELETE ON agent_task_wake_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT, UPDATE ON agent_task TO hcmnext_app;
GRANT SELECT, INSERT ON agent_task_wake_receipt TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_task) THEN
        RAISE EXCEPTION 'cannot remove retained agent tasks';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_task_wake_receipt;
DROP TABLE agent_task;
