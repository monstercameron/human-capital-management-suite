-- AGENT2-025 prerequisite: durable task-scoped plan and approval observations.

-- +goose Up

CREATE TABLE agent_task_event (
    tenant_id       uuid        NOT NULL REFERENCES tenant(tenant_id),
    task_id         text        NOT NULL,
    event_sequence  bigint      NOT NULL CHECK (event_sequence > 0),
    event_type      text        NOT NULL CHECK (event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION', 'APPROVAL_REQUESTED', 'APPROVAL_OUTCOME')),
    plan_revision   bigint,
    plan_digest     text,
    step_id         text,
    approval_digest text,
    outcome         text        NOT NULL CHECK (btrim(outcome) <> ''),
    actor_id        text        NOT NULL DEFAULT '',
    occurred_at     timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, task_id, event_sequence),
    FOREIGN KEY (tenant_id, task_id) REFERENCES agent_task (tenant_id, task_id),
    CHECK (
        (event_type IN ('PLAN_REVISION', 'PLAN_CONFIRMATION')
            AND plan_revision IS NOT NULL AND plan_revision > 0 AND plan_digest IS NOT NULL AND btrim(plan_digest) <> ''
            AND step_id IS NULL AND approval_digest IS NULL)
        OR
        (event_type IN ('APPROVAL_REQUESTED', 'APPROVAL_OUTCOME')
            AND plan_revision IS NULL AND plan_digest IS NULL AND step_id IS NOT NULL AND btrim(step_id) <> ''
            AND approval_digest IS NOT NULL AND btrim(approval_digest) <> '')
    )
);

ALTER TABLE agent_task_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_task_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_task_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_task_event_immutable
    BEFORE UPDATE OR DELETE ON agent_task_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON agent_task_event TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_task_event) THEN
        RAISE EXCEPTION 'agent task event history is retained; cannot drop it';
    END IF;
END $$;
-- +goose StatementEnd
REVOKE ALL ON agent_task_event FROM hcmnext_app;
DROP POLICY tenant_isolation ON agent_task_event;
ALTER TABLE agent_task_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE agent_task_event DISABLE ROW LEVEL SECURITY;
DROP TRIGGER agent_task_event_immutable ON agent_task_event;
DROP TABLE agent_task_event;
