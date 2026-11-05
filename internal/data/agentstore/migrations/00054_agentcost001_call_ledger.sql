-- AGENTCOST-001: one sealed row per model call beside the stage checkpoints,
-- so spend regenerates from run records. Identifiers and digests only; never
-- prompts or answers.
-- +goose Up
CREATE TABLE agent_run_model_call (
    tenant_id           uuid        NOT NULL,
    run_id              text        NOT NULL,
    sequence            bigint      NOT NULL CHECK (sequence > 0),
    trigger_kind        text        NOT NULL DEFAULT '',
    question_id         text        NOT NULL DEFAULT '',
    call_site           text        NOT NULL CHECK (btrim(call_site) <> ''),
    stage               text        NOT NULL CHECK (stage IN ('admission','answer','tool_continuation','validation','evaluation','announcement','screening')),
    kind                text        NOT NULL CHECK (kind IN ('generation','extraction','decision')),
    provider_id         text        NOT NULL DEFAULT '',
    model_id            text        NOT NULL DEFAULT '',
    model_version       text        NOT NULL DEFAULT '',
    input_tokens        bigint      NOT NULL CHECK (input_tokens >= 0),
    output_tokens       bigint      NOT NULL CHECK (output_tokens >= 0),
    cached_input_tokens bigint      NOT NULL CHECK (cached_input_tokens >= 0),
    cost_micros         bigint      NOT NULL CHECK (cost_micros >= 0),
    cost_basis          text        NOT NULL CHECK (cost_basis IN ('measured','estimated')),
    latency_ms          bigint      NOT NULL CHECK (latency_ms >= 0),
    started_at          timestamptz NOT NULL,
    prompt_digest       text        NOT NULL DEFAULT '',
    schedule_version    text        NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, run_id, sequence),
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_run_execution (tenant_id, run_id),
    CHECK (cached_input_tokens <= input_tokens)
);
CREATE INDEX agent_run_model_call_site ON agent_run_model_call (tenant_id, run_id, call_site, sequence);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agent_run_model_call
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE agent_run_model_call ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_run_model_call FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_run_model_call
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON agent_run_model_call TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM agent_run_model_call) THEN
        RAISE EXCEPTION 'cannot remove retained model call evidence';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_run_model_call;
