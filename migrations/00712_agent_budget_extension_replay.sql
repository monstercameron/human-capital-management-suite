-- AGENT2-012/017: revision-fenced, replay-safe budget extensions.

-- +goose Up

ALTER TABLE agent_budget_task
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0);

ALTER TABLE agent_budget_event
    ADD COLUMN request_id text NOT NULL DEFAULT '',
    ADD COLUMN expected_revision bigint NOT NULL DEFAULT 0 CHECK (expected_revision >= 0),
    ADD COLUMN result_revision bigint NOT NULL DEFAULT 0 CHECK (result_revision >= 0);

CREATE TABLE agent_budget_extension_replay (
    tenant_id        uuid        NOT NULL REFERENCES tenant(tenant_id),
    task_id          text        NOT NULL,
    request_id       text        NOT NULL CHECK (btrim(request_id) <> '' AND length(request_id) <= 200),
    expected_revision bigint     NOT NULL CHECK (expected_revision > 0),
    result_revision   bigint     NOT NULL CHECK (result_revision > expected_revision),
    additional        jsonb      NOT NULL CHECK (jsonb_typeof(additional) = 'object'),
    result_limits     jsonb      NOT NULL CHECK (jsonb_typeof(result_limits) = 'object'),
    additional_metadata jsonb    NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(additional_metadata) = 'object'),
    created_at        timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, task_id, request_id),
    FOREIGN KEY (tenant_id, task_id) REFERENCES agent_budget_task (tenant_id, task_id)
);

ALTER TABLE agent_budget_extension_replay ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_budget_extension_replay FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_budget_extension_replay
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER agent_budget_extension_replay_immutable
    BEFORE UPDATE OR DELETE ON agent_budget_extension_replay
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON agent_budget_extension_replay TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_budget_extension_replay)
       OR EXISTS (SELECT 1 FROM agent_budget_task)
       OR EXISTS (SELECT 1 FROM agent_budget_event) THEN
        RAISE EXCEPTION 'cannot remove retained agent budget revision or replay rows';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_budget_extension_replay;
ALTER TABLE agent_budget_event
    DROP COLUMN result_revision,
    DROP COLUMN expected_revision,
    DROP COLUMN request_id;
ALTER TABLE agent_budget_task DROP COLUMN revision;
