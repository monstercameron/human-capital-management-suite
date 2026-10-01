-- AGENT2-012: durable agent budget ledger. Task specs with settled usage, the
-- pause card and the attempt/failure counters, plus per-user-day and
-- per-tenant-month period usage. In-flight reservations are not stored: a
-- restart releases them.

-- +goose Up

CREATE TABLE agent_budget_task (
    tenant_id     uuid        NOT NULL REFERENCES tenant(tenant_id),
    task_id       text        NOT NULL CHECK (btrim(task_id) <> ''),
    tenant_ref    text        NOT NULL CHECK (btrim(tenant_ref) <> ''),
    user_id       text        NOT NULL CHECK (btrim(user_id) <> ''),
    limit_steps   bigint      NOT NULL CHECK (limit_steps > 0),
    limit_tokens  bigint      NOT NULL CHECK (limit_tokens > 0),
    limit_wall_ns bigint      NOT NULL CHECK (limit_wall_ns > 0),
    limit_spend   bigint      NOT NULL CHECK (limit_spend > 0),
    used_steps    bigint      NOT NULL DEFAULT 0 CHECK (used_steps >= 0),
    used_tokens   bigint      NOT NULL DEFAULT 0 CHECK (used_tokens >= 0),
    used_wall_ns  bigint      NOT NULL DEFAULT 0 CHECK (used_wall_ns >= 0),
    used_spend    bigint      NOT NULL DEFAULT 0 CHECK (used_spend >= 0),
    pause_reason  text        NOT NULL DEFAULT '',
    pause_card    jsonb       CHECK (pause_card IS NULL OR jsonb_typeof(pause_card) = 'object'),
    attempts      jsonb       NOT NULL CHECK (jsonb_typeof(attempts) = 'object'),
    failures      jsonb       NOT NULL CHECK (jsonb_typeof(failures) = 'object'),
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, task_id),
    CHECK ((pause_card IS NULL) = (pause_reason = ''))
);

CREATE TABLE agent_budget_period (
    tenant_id    uuid        NOT NULL REFERENCES tenant(tenant_id),
    scope        text        NOT NULL CHECK (scope IN ('USER_DAILY', 'TENANT_MONTHLY')),
    period_key   text        NOT NULL CHECK (btrim(period_key) <> ''),
    period_start date        NOT NULL,
    used_steps   bigint      NOT NULL DEFAULT 0 CHECK (used_steps >= 0),
    used_tokens  bigint      NOT NULL DEFAULT 0 CHECK (used_tokens >= 0),
    used_wall_ns bigint      NOT NULL DEFAULT 0 CHECK (used_wall_ns >= 0),
    used_spend   bigint      NOT NULL DEFAULT 0 CHECK (used_spend >= 0),
    updated_at   timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, scope, period_key)
);

CREATE INDEX agent_budget_period_start ON agent_budget_period (tenant_id, period_start DESC);

-- Append-only journal of every durable budget decision.
CREATE TABLE agent_budget_event (
    tenant_id      uuid        NOT NULL REFERENCES tenant(tenant_id),
    event_seq      bigint      GENERATED ALWAYS AS IDENTITY,
    task_id        text        NOT NULL,
    kind           text        NOT NULL CHECK (kind IN ('OPEN_TASK', 'SETTLE', 'FAIL', 'EXTENSION', 'PAUSE')),
    reservation_id text        NOT NULL DEFAULT '',
    step_id        text        NOT NULL DEFAULT '',
    steps          bigint      NOT NULL DEFAULT 0 CHECK (steps >= 0),
    tokens         bigint      NOT NULL DEFAULT 0 CHECK (tokens >= 0),
    wall_ns        bigint      NOT NULL DEFAULT 0 CHECK (wall_ns >= 0),
    spend          bigint      NOT NULL DEFAULT 0 CHECK (spend >= 0),
    pause_reason   text        NOT NULL DEFAULT '',
    occurred_at    timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, event_seq),
    FOREIGN KEY (tenant_id, task_id) REFERENCES agent_budget_task (tenant_id, task_id)
);

CREATE INDEX agent_budget_event_task ON agent_budget_event (tenant_id, task_id, event_seq);

ALTER TABLE agent_budget_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_budget_task FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_budget_task
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE agent_budget_period ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_budget_period FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_budget_period
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE agent_budget_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_budget_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_budget_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_budget_event_immutable
    BEFORE UPDATE OR DELETE ON agent_budget_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT, UPDATE ON agent_budget_task, agent_budget_period TO hcmnext_app;
GRANT SELECT, INSERT ON agent_budget_event TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_budget_task) OR EXISTS (SELECT 1 FROM agent_budget_period) THEN
        RAISE EXCEPTION 'cannot remove retained agent budget ledger rows';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_budget_event;
DROP TABLE agent_budget_period;
DROP TABLE agent_budget_task;
